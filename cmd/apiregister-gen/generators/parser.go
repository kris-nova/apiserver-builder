/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package generators

import (
	"fmt"
	"log"
	"path"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/gengo/args"
	"k8s.io/gengo/generator"
	"k8s.io/gengo/types"
)

type APIs struct {
	// Domain is the domain portion of the group - e.g. k8s.io
	Domain string
	// Package is the name of the go package the api group is under - e.g. github.com/pwittrock/apiserver-helloworld/apis
	Package string
	Pkg     *types.Package
	// Groups is a list of API groups
	Groups map[string]*APIGroup
}

type Controller struct {
	Target   schema.GroupVersionKind
	Resource string
	Pkg      *types.Package
	Repo     string
}

type APIGroup struct {
	// Package is the name of the go package the api group is under - e.g. github.com/pwittrock/apiserver-helloworld/apis
	Package string
	// Domain is the domain portion of the group - e.g. k8s.io
	Domain string
	// Group is the short name of the group - e.g. mushroomkingdom
	Group      string
	GroupTitle string
	// Versions is the list of all versions for this group keyed by name
	Versions map[string]*APIVersion

	UnversionedResources map[string]*APIResource

	// Structs is a list of unversioned definitions that must be generated
	Structs []*Struct
	Pkg     *types.Package
	PkgPath string
}

type Struct struct {
	// Name is the name of the type
	Name string
	// GenClient
	GenClient   bool
	GenDeepCopy bool

	GenUnversioned bool
	// Fields is the list of fields appearing in the struct
	Fields []*Field
}

type Field struct {
	// Name is the name of the field
	Name string
	// For versioned Kubernetes types, this is the versioned package
	VersionedPackage string
	// For versioned Kubernetes types, this is theun versioned package
	UnversionedImport string
	UnversionedType   string
}

type APIVersion struct {
	// Domain is the group domain - e.g. k8s.io
	Domain string
	// Group is the group name - e.g. mushroomkingdom
	Group string
	// Version is the api version - e.g. v1beta1
	Version string
	// Resources is a list of resources appearing in the API version keyed by name
	Resources map[string]*APIResource
	// Pkg is the Package object from code-gen
	Pkg *types.Package
}

type APIResource struct {
	// Domain is the group domain - e.g. k8s.io
	Domain string
	// Group is the group name - e.g. mushroomkingdom
	Group string
	// Version is the api version - e.g. v1beta1
	Version string
	// Kind is the resource name - e.g. PeachesCastle
	Kind string
	// Resource is the resource name - e.g. peachescastles
	Resource string
	// REST is the rest.Storage implementation used to handle requests
	// This field is optional. The standard REST implementation will be used
	// by default.
	REST string
	// Subresources is a map of subresources keyed by name
	Subresources map[string]*APISubresource
	// Type is the Type object from code-gen
	Type *types.Type
	// Strategy is name of the struct to use for the strategy
	Strategy string
	// Strategy is name of the struct to use for the strategy
	StatusStrategy string
	// NonNamespaced indicates that the resource kind is non namespaced
	NonNamespaced bool
}

type APISubresource struct {
	// Domain is the group domain - e.g. k8s.io
	Domain string
	// Group is the group name - e.g. mushroomkingdom
	Group string
	// Version is the api version - e.g. v1beta1
	Version string
	// Kind is the resource name - e.g. PeachesCastle
	Kind string
	// Resource is the resource name - e.g. peachescastles
	Resource string
	// Request is the subresource request type - e.g. ScaleCastle
	Request string
	// REST is the rest.Storage implementation used to handle requests
	REST string
	// Path is the subresource path - e.g. scale
	Path string

	// ImportPackage is the import statement that must appear for the Request
	ImportPackage string

	// RequestType is the type of the request
	RequestType *types.Type

	// RESTType is the type of the request handler
	RESTType *types.Type
}

type APIsBuilder struct {
	context         *generator.Context
	arguments       *args.GeneratorArgs
	Domain          string
	VersionedPkgs   sets.String
	UnversionedPkgs sets.String
	APIsPkg         string
	APIsPkgRaw      *types.Package
	GroupNames      sets.String

	APIs        *APIs
	Controllers []Controller

	ByGroupKindVersion    map[string]map[string]map[string]*APIResource
	ByGroupVersionKind    map[string]map[string]map[string]*APIResource
	SubByGroupVersionKind map[string]map[string]map[string]*types.Type
	Groups                map[string]types.Package
}

func NewAPIsBuilder(context *generator.Context, arguments *args.GeneratorArgs) *APIsBuilder {
	b := &APIsBuilder{
		context:   context,
		arguments: arguments,
	}
	b.ParsePackages()
	b.ParseDomain()
	b.ParseGroupNames()
	b.ParseIndex()
	b.ParseControllers()
	b.ParseAPIs()

	return b
}

func (b *APIsBuilder) ParseControllers() {
	for _, c := range b.context.Order {
		if IsController(c) {
			tags := ParseControllerTag(b.GetControllerTag(c))
			repo := strings.Split(c.Name.Package, "/pkg/controller")[0]
			pkg := b.context.Universe[c.Name.Package]
			b.Controllers = append(b.Controllers, Controller{
				tags.gvk, tags.resource, pkg, repo})
		}
	}
}

func (b *APIsBuilder) ParseAPIs() {
	apis := &APIs{
		Domain:  b.Domain,
		Package: b.APIsPkg,
		Groups:  map[string]*APIGroup{},
	}

	for group, versionMap := range b.ByGroupVersionKind {
		apiGroup := &APIGroup{
			Group:                group,
			GroupTitle:           strings.Title(group),
			Domain:               b.Domain,
			Versions:             map[string]*APIVersion{},
			UnversionedResources: map[string]*APIResource{},
		}

		for version, kindMap := range versionMap {
			apiVersion := &APIVersion{
				Domain:    b.Domain,
				Group:     group,
				Version:   version,
				Resources: map[string]*APIResource{},
			}
			for kind, resource := range kindMap {
				apiResource := &APIResource{
					Domain:         resource.Domain,
					Version:        resource.Version,
					Group:          resource.Group,
					Resource:       resource.Resource,
					Type:           resource.Type,
					REST:           resource.REST,
					Kind:           resource.Kind,
					Subresources:   resource.Subresources,
					StatusStrategy: resource.StatusStrategy,
					Strategy:       resource.Strategy,
					NonNamespaced:  resource.NonNamespaced,
				}
				apiVersion.Resources[kind] = apiResource
				// Set the package for the api version
				apiVersion.Pkg = b.context.Universe[resource.Type.Name.Package]
				// Set the package for the api group
				apiGroup.Pkg = b.context.Universe[filepath.Dir(resource.Type.Name.Package)]
				apiGroup.PkgPath = apiGroup.Pkg.Path

				apiGroup.UnversionedResources[kind] = apiResource
			}

			apiGroup.Versions[version] = apiVersion
		}
		b.ParseStructs(apiGroup)
		apis.Groups[group] = apiGroup
	}
	apis.Pkg = b.context.Universe[b.APIsPkg]
	b.APIs = apis
}

// ParseIndex indexes all types with the comment "// +resource=RESOURCE" by GroupVersionKind and
// GroupKindVersion
func (b *APIsBuilder) ParseIndex() {
	b.ByGroupVersionKind = map[string]map[string]map[string]*APIResource{}
	b.ByGroupKindVersion = map[string]map[string]map[string]*APIResource{}

	b.SubByGroupVersionKind = map[string]map[string]map[string]*types.Type{}
	for _, c := range b.context.Order {
		if IsAPISubresource(c) {
			group := GetGroup(c)
			version := GetVersion(c, group)
			kind := GetKind(c, group)
			if _, f := b.SubByGroupVersionKind[group]; !f {
				b.SubByGroupVersionKind[group] = map[string]map[string]*types.Type{}
			}
			if _, f := b.SubByGroupVersionKind[group][version]; !f {
				b.SubByGroupVersionKind[group][version] = map[string]*types.Type{}
			}
			b.SubByGroupVersionKind[group][version][kind] = c
		}

		if !IsAPIResource(c) {
			continue
		}

		r := &APIResource{
			Type:          c,
			NonNamespaced: IsNonNamespaced(c),
		}
		r.Group = GetGroup(c)
		r.Version = GetVersion(c, r.Group)
		r.Kind = GetKind(c, r.Group)
		r.Domain = b.Domain

		rt := ParseResourceTag(b.GetResourceTag(c))

		r.Resource = rt.Resource
		r.REST = rt.REST

		r.Strategy = rt.Strategy

		// If not defined, default the strategy to the group strategy for backwards compatibility
		if len(r.Strategy) == 0 {
			r.Strategy = fmt.Sprintf("%s.%sStrategy", r.Group, r.Kind)
		}

		// Copy the Status strategy to mirror the non-status strategy
		r.StatusStrategy = strings.TrimSuffix(r.Strategy, "Strategy")
		r.StatusStrategy = fmt.Sprintf("%sStatusStrategy", r.StatusStrategy)

		if _, f := b.ByGroupKindVersion[r.Group]; !f {
			b.ByGroupKindVersion[r.Group] = map[string]map[string]*APIResource{}
		}
		if _, f := b.ByGroupKindVersion[r.Group][r.Kind]; !f {
			b.ByGroupKindVersion[r.Group][r.Kind] = map[string]*APIResource{}
		}
		if _, f := b.ByGroupVersionKind[r.Group]; !f {
			b.ByGroupVersionKind[r.Group] = map[string]map[string]*APIResource{}
		}
		if _, f := b.ByGroupVersionKind[r.Group][r.Version]; !f {
			b.ByGroupVersionKind[r.Group][r.Version] = map[string]*APIResource{}
		}

		b.ByGroupKindVersion[r.Group][r.Kind][r.Version] = r
		b.ByGroupVersionKind[r.Group][r.Version][r.Kind] = r

		// Do subresources
		if !HasSubresource(c) {
			continue
		}
		r.Type = c
		r.Subresources = b.GetSubresources(r)
	}
}

func (b *APIsBuilder) GetSubresources(c *APIResource) map[string]*APISubresource {
	r := map[string]*APISubresource{}
	subresources := b.GetSubresourceTags(c.Type)

	if len(subresources) == 0 {
		// Not a subresource
		return r
	}
	for _, subresource := range subresources {
		// Parse the values for each subresource
		tags := ParseSubresourceTag(c, subresource)
		sr := &APISubresource{
			Kind:     tags.Kind,
			Request:  tags.RequestKind,
			Path:     tags.Path,
			REST:     tags.REST,
			Domain:   b.Domain,
			Version:  c.Version,
			Resource: c.Resource,
			Group:    c.Group,
		}
		if !b.IsInPackage(tags) {
			// Out of package Request types require an import and are prefixed with the
			// package name - e.g. v1.Scale
			sr.Request, sr.ImportPackage = b.GetNameAndImport(tags)
		}
		if v, found := r[sr.Path]; found {
			log.Fatalf("Multiple subresources registered for path %s: %v %v",
				sr.Path, v, subresource)
		}
		r[sr.Path] = sr
	}
	return r
}

// Returns true if the subresource Request type is in the same package as the resource type
func (b *APIsBuilder) IsInPackage(tags SubresourceTags) bool {
	return !strings.Contains(tags.RequestKind, ".")
}

// GetNameAndImport converts
func (b *APIsBuilder) GetNameAndImport(tags SubresourceTags) (string, string) {
	last := strings.LastIndex(tags.RequestKind, ".")
	importPackage := tags.RequestKind[:last]

	// Set the request kind to the struct name
	tags.RequestKind = tags.RequestKind[last+1:]
	// Find the package
	pkg := filepath.Base(importPackage)
	// Prefix the struct name with the package it is in
	return strings.Join([]string{pkg, tags.RequestKind}, "."), importPackage
}

// ResourceTags contains the tags present in a "+resource=" comment
type ResourceTags struct {
	Resource string
	REST     string
	Strategy string
}

// ParseResourceTag parses the tags in a "+resource=" comment into a ResourceTags struct
func ParseResourceTag(tag string) ResourceTags {
	result := ResourceTags{}
	for _, elem := range strings.Split(tag, ",") {
		kv := strings.Split(elem, "=")
		if len(kv) != 2 {
			log.Fatalf("// +resource: tags must be key value pairs.  Expected "+
				"keys [path=<subresourcepath>] "+
				"Got string: [%s]", tag)
		}
		value := kv[1]
		switch kv[0] {
		case "rest":
			result.REST = value
		case "path":
			result.Resource = value
		case "strategy":
			result.Strategy = value
		}
	}
	return result
}

// ResourceTags contains the tags present in a "+resource=" comment
type ControllerTags struct {
	gvk      schema.GroupVersionKind
	resource string
}

// ParseResourceTag parses the tags in a "+resource=" comment into a ResourceTags struct
func ParseControllerTag(tag string) ControllerTags {
	result := ControllerTags{}
	for _, elem := range strings.Split(tag, ",") {
		kv := strings.Split(elem, "=")
		if len(kv) != 2 {
			log.Fatalf("// +controller: tags must be key value pairs.  Expected "+
				"keys [group=<group>,version=<version>,kind=<kind>,resource=<resource>] "+
				"Got string: [%s]", tag)
		}
		value := kv[1]
		switch kv[0] {
		case "group":
			result.gvk.Group = value
		case "version":
			result.gvk.Version = value
		case "kind":
			result.gvk.Kind = value
		case "resource":
			result.resource = value
		}
	}
	return result
}

// SubresourceTags contains the tags present in a "+subresource=" comment
type SubresourceTags struct {
	Path        string
	Kind        string
	RequestKind string
	REST        string
}

// ParseSubresourceTag parses the tags in a "+subresource=" comment into a SubresourceTags struct
func ParseSubresourceTag(c *APIResource, tag string) SubresourceTags {
	result := SubresourceTags{}
	for _, elem := range strings.Split(tag, ",") {
		kv := strings.Split(elem, "=")
		if len(kv) != 2 {
			log.Fatalf("// +subresource: tags must be key value pairs.  Expected "+
				"keys [request=<requestType>,rest=<restImplType>,path=<subresourcepath>] "+
				"Got string: [%s]", tag)
		}
		value := kv[1]
		switch kv[0] {
		case "request":
			result.RequestKind = value
		case "rest":
			result.REST = value
		case "path":
			// Strip the parent resource
			result.Path = strings.Replace(value, c.Resource+"/", "", -1)
		}
	}
	return result
}

// GetResourceTag returns the value of the "+resource=" comment tag
func (b *APIsBuilder) GetResourceTag(c *types.Type) string {
	comments := Comments(c.CommentLines)
	resource := comments.GetTag("resource", ":")
	if len(resource) == 0 {
		panic(errors.Errorf("Must specify +resource comment for type %v", c.Name))
	}
	return resource
}

func (b *APIsBuilder) GenClient(c *types.Type) bool {
	comments := Comments(c.CommentLines)
	resource := comments.GetTag("resource", ":")
	return len(resource) > 0
}

func (b *APIsBuilder) GenDeepCopy(c *types.Type) bool {
	comments := Comments(c.CommentLines)
	return comments.HasTag("subresource-request")
}

func (b *APIsBuilder) GetControllerTag(c *types.Type) string {
	comments := Comments(c.CommentLines)
	resource := comments.GetTag("controller", ":")
	if len(resource) == 0 {
		panic(errors.Errorf("Must specify +controller comment for type %v", c.Name))
	}
	return resource
}

func (b *APIsBuilder) GetSubresourceTags(c *types.Type) []string {
	comments := Comments(c.CommentLines)
	return comments.GetTags("subresource", ":")
}

// ParseGroupNames initializes b.GroupNames with the set of all groups
func (b *APIsBuilder) ParseGroupNames() {
	b.GroupNames = sets.String{}
	for p := range b.UnversionedPkgs {
		pkg := b.context.Universe[p]
		if pkg == nil {
			// If the input had no Go files, for example.
			continue
		}
		b.GroupNames.Insert(filepath.Base(p))
	}
}

// ParsePackages parses out the sets of Versioned, Unversioned packages and identifies the root Apis package.
func (b *APIsBuilder) ParsePackages() {
	b.VersionedPkgs = sets.NewString()
	b.UnversionedPkgs = sets.NewString()
	for _, o := range b.context.Order {
		if IsAPIResource(o) {
			versioned := o.Name.Package
			b.VersionedPkgs.Insert(versioned)

			unversioned := filepath.Dir(versioned)
			b.UnversionedPkgs.Insert(unversioned)

			if apis := filepath.Dir(unversioned); apis != b.APIsPkg && len(b.APIsPkg) > 0 {
				panic(errors.Errorf(
					"Found multiple apis directory paths: %v and %v.  "+
						"Do you have a +resource tag on a resource that is not in a version "+
						"directory?", b.APIsPkg, apis))
			} else {
				b.APIsPkg = apis
			}
		}
	}
}

// ParseDomain parses the domain from the apis/doc.go file comment "// +domain=YOUR_DOMAIN".
func (b *APIsBuilder) ParseDomain() {
	pkg := b.context.Universe[b.APIsPkg]
	if pkg == nil {
		// If the input had no Go files, for example.
		panic(errors.Errorf("Missing apis package."))
	}
	comments := Comments(pkg.Comments)
	b.Domain = comments.GetTag("domain", "=")
	if len(b.Domain) == 0 {
		panic("Could not find string matching // +domain=.+ in apis/doc.go")
	}
}

func (b *APIsBuilder) ParseStructs(apigroup *APIGroup) {
	remaining := []*types.Type{}
	for _, version := range apigroup.Versions {
		for _, resource := range version.Resources {
			remaining = append(remaining, resource.Type)
		}
	}
	for _, version := range b.SubByGroupVersionKind[apigroup.Group] {
		for _, kind := range version {
			remaining = append(remaining, kind)
		}
	}

	done := sets.String{}
	for len(remaining) > 0 {
		// Pop the next element from the list
		next := remaining[0]
		remaining[0] = remaining[len(remaining)-1]
		remaining = remaining[:len(remaining)-1]

		// Already processed this type.  Skip it
		if done.Has(next.Name.Name) {
			continue
		}
		done.Insert(next.Name.Name)

		// Generate the struct and append to the list
		result, additionalTypes := apigroup.DoType(next)

		// This is a resource, so generate the client
		if b.GenClient(next) {
			result.GenClient = true
			result.GenDeepCopy = true
		}

		if b.GenDeepCopy(next) {
			result.GenDeepCopy = true
		}
		apigroup.Structs = append(apigroup.Structs, result)

		// Add the newly discovered subtypes
		remaining = append(remaining, additionalTypes...)
	}
}

func (apigroup *APIGroup) DoType(t *types.Type) (*Struct, []*types.Type) {
	remaining := []*types.Type{}

	s := &Struct{
		Name:           t.Name.Name,
		GenClient:      false,
		GenUnversioned: true, // Generate unversioned structs by default
	}

	for _, c := range t.CommentLines {
		if strings.Contains(c, "+genregister:unversioned=false") {
			// Don't generate the unversioned struct
			s.GenUnversioned = false
		}
	}

	for _, member := range t.Members {
		memberGroup := GetGroup(member.Type)
		uType := member.Type.Name.Name
		memberName := member.Name
		uImport := ""

		base := filepath.Base(member.Type.String())
		samepkg := t.Name.Package == member.Type.Name.Package

		// If not in the same package, calculate the import pkg
		if !samepkg {
			parts := strings.Split(base, ".")
			if len(parts) > 1 {
				switch member.Type.Name.Package {
				case "k8s.io/apimachinery/pkg/apis/meta/v1":
					// Use versioned types for meta/v1
					uImport = fmt.Sprintf("%s \"%s\"", "metav1", "k8s.io/apimachinery/pkg/apis/meta/v1")
					uType = "metav1." + parts[1]
				default:
					// Use unversioned types for everything else
					t := member.Type
					hasElem := false
					if t.Elem != nil {
						// Handle Pointers, Maps, Slices correctly
						t = t.Elem
						hasElem = true
					}
					uImportName := path.Base(path.Dir(t.Name.Package))
					uImport = path.Dir(t.Name.Package)
					uType = uImportName + "." + t.Name.Name
					if hasElem {
						uType = strings.Replace(member.Type.String(), path.Dir(uImport)+"/", "", 1)
						uType = strings.Replace(uType, "/"+path.Base(t.Name.Package), "", 1)
					}
					fmt.Printf("\n%s : %s : %s\n%s : %s\n\n", member.Type.Kind, member.Type.String(), path.Dir(uImport), uImport, uType)
				}
			}
		}

		if member.Embedded {
			memberName = ""
		}

		s.Fields = append(s.Fields, &Field{
			Name:              memberName,
			VersionedPackage:  member.Type.Name.Package,
			UnversionedImport: uImport,
			UnversionedType:   uType,
		})

		// Add this member Type for processing
		if !member.Type.IsPrimitive() && memberGroup == GetGroup(t) {
			remaining = append(remaining, member.Type)
		}
	}
	return s, remaining
}

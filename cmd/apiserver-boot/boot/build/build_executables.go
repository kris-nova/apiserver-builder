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

package build

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var GenerateForBuild bool = true
var goos string = "linux"
var goarch string = "amd64"
var outputdir string = "bin"

var createBuildExecutablesCmd = &cobra.Command{
	Use:   "executables",
	Short: "Builds the source into executables to run on the local machine",
	Long:  `Builds the source into executables to run on the local machine`,
	Example: `# Generate code and build the apiserver and controller
# binaries in the bin directory so they can be run locally.
apiserver-boot build executables

# Build binaries into the linux/ directory using the cross compiler for linux:amd64
apiserver-boot build --goos linux --goarch amd64 --output linux/`,
	Run: RunBuildExecutables,
}

func AddBuildExecutables(cmd *cobra.Command) {
	cmd.AddCommand(createBuildExecutablesCmd)

	createBuildExecutablesCmd.Flags().BoolVar(&GenerateForBuild, "generate", true, "if true, generate code before building")
	createBuildExecutablesCmd.Flags().StringVar(&goos, "goos", "", "if specified, set this GOOS")
	createBuildExecutablesCmd.Flags().StringVar(&goarch, "goarch", "", "if specified, set this GOARCH")
	createBuildExecutablesCmd.Flags().StringVar(&outputdir, "output", "bin", "if set, write the binaries to this directory")
}

func RunBuildExecutables(cmd *cobra.Command, args []string) {
	if GenerateForBuild {
		log.Printf("regenerating generated code.  To disable regeneration, run with --generate=false.")
		RunGenerate(cmd, args)
	}

	// Build the apiserver
	path := filepath.Join("cmd", "apiserver", "main.go")
	c := exec.Command("go", "build", "-o", filepath.Join(outputdir, "apiserver"), path)
	c.Env = append(os.Environ(), "CGO_ENABLED=0")
	log.Printf("CGO_ENABLED=0")
	if len(goos) > 0 {
		c.Env = append(c.Env, fmt.Sprintf("GOOS=%s", goos))
		log.Printf(fmt.Sprintf("GOOS=%s", goos))
	}
	if len(goarch) > 0 {
		c.Env = append(c.Env, fmt.Sprintf("GOARCH=%s", goarch))
		log.Printf(fmt.Sprintf("GOARCH=%s", goarch))
	}

	fmt.Printf("%s\n", strings.Join(c.Args, " "))
	c.Stderr = os.Stderr
	c.Stdout = os.Stdout
	err := c.Run()
	if err != nil {
		log.Fatal(err)
	}

	// Build the controller manager
	path = filepath.Join("cmd", "controller-manager", "main.go")
	c = exec.Command("go", "build", "-o", filepath.Join(outputdir, "controller-manager"), path)
	c.Env = append(os.Environ(), "CGO_ENABLED=0")
	if len(goos) > 0 {
		c.Env = append(c.Env, fmt.Sprintf("GOOS=%s", goos))
	}
	if len(goarch) > 0 {
		c.Env = append(c.Env, fmt.Sprintf("GOARCH=%s", goarch))
	}

	fmt.Println(strings.Join(c.Args, " "))
	c.Stderr = os.Stderr
	c.Stdout = os.Stdout
	err = c.Run()
	if err != nil {
		log.Fatal(err)
	}
}

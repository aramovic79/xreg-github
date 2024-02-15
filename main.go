package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	log "github.com/duglin/dlog"
	"github.com/duglin/xreg-github/registry"
)

func init() {
	log.SetVerbose(2)
}

var Port = 8080
var DBName = "registry"
var Verbose = 2

func main() {
	var err error

	doDelete := flag.Bool("delete", false, "Delete DB an exit")
	doRecreate := flag.Bool("recreate", false, "Recreate DB, then run")
	flag.IntVar(&Verbose, "v", 2, "Verbose level")
	flag.Parse()

	log.SetVerbose(Verbose)

	if *doDelete || *doRecreate {
		err := registry.DeleteDB(DBName)
		if err != nil {
			panic(err)
		}
		if *doDelete {
			os.Exit(0)
		}
	}

	if !registry.DBExists(DBName) {
		registry.CreateDB(DBName)
	}

	registry.OpenDB(DBName)

	// testing
	if 0 == 1 {
		reg, err := registry.NewRegistry("test")
		ErrFatalf(err)
		gm, err := reg.Model.AddGroupModel("dirs", "dir")
		ErrFatalf(err)
		_, err = gm.AddResourceModel("files", "file", 2, true, true, true)
		ErrFatalf(err)

		g, err := reg.AddGroup("dirs", "dir1")
		r, err := g.AddResource("files", "f1", "v1")
		v1, err := r.FindVersion("v1")
		r.AddVersion("v2")
		v1.Refresh()
		v1.Set("name", "myname")
		os.Exit(0)
	}

	// e-testing

	reg, err := registry.FindRegistry("SampleRegistry")
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(1)
	}

	if reg == nil {
		reg = LoadDirsSample(reg)
		LoadEndpointsSample(nil)
		LoadMessagesSample(nil)
		LoadSchemasSample(nil)
		LoadAPIGuru(nil, "APIs-guru", "openapi-directory")
	}

	if reg == nil {
		fmt.Fprintf(os.Stderr, "No registry loaded\n")
		os.Exit(1)
	}

	if tmp := os.Getenv("PORT"); tmp != "" {
		tmpInt, _ := strconv.Atoi(tmp)
		if tmpInt != 0 {
			Port = tmpInt
		}
	}

	registry.DefaultReg = reg
	registry.NewServer(Port).Serve()
}

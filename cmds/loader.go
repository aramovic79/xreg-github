package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/duglin/dlog"
	"github.com/duglin/xreg-github/registry"
)

var Token string
var Secret string

func ErrFatalf(err error, args ...any) {
	if err == nil {
		return
	}
	format := "%s"
	if len(args) > 0 {
		format = args[0].(string)
		args = args[1:]
	} else {
		args = []any{err}
	}
	log.Printf(format, args...)
	registry.ShowStack()
	os.Exit(1)
}

func init() {
	if tmp := os.Getenv("githubToken"); tmp != "" {
		Token = tmp
	} else {
		if buf, _ := os.ReadFile(".github"); len(buf) > 0 {
			Token = string(buf)
		}
	}
}

func LoadAPIGuru(reg *registry.Registry, orgName string, repoName string) *registry.Registry {
	var err error
	log.VPrintf(1, "Loading registry '%s/%s'", orgName, repoName)
	Token = strings.TrimSpace(Token)

	/*
		gh := github.NewGitHubClient("api.github.com", Token, Secret)
		repo, err := gh.GetRepository(orgName, repoName)
		if err != nil {
			log.Fatalf("Error finding repo %s/%s: %s", orgName, repoName, err)
		}

		tarStream, err := repo.GetTar()
		if err != nil {
			log.Fatalf("Error getting tar from repo %s/%s: %s",
				orgName, repoName, err)
		}
		defer tarStream.Close()
	*/

	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Error getting current working directory: %s", err)
	}
	tarPath := filepath.Join(filepath.Dir(wd), "misc", "repo.tar")

	buf, err := os.ReadFile(tarPath)

	if err != nil {
		log.Fatalf("Can't load 'misc/repo.tar': %s", err)
	}
	tarStream := bytes.NewReader(buf)

	gzf, _ := gzip.NewReader(tarStream)
	reader := tar.NewReader(gzf)

	if reg == nil {
		reg, err = registry.FindRegistry(nil, "API-Guru")
		ErrFatalf(err)
		if reg != nil {
			reg.Rollback()
			return reg
		}

		reg, err = registry.NewRegistry(nil, "API-Guru")
		ErrFatalf(err, "Error creating new registry: %s", err)
		// log.VPrintf(3, "New registry:\n%#v", reg)
		defer reg.Rollback()

		ErrFatalf(reg.SetSave("#baseURL", "http://soaphub.org:8585/"))
		ErrFatalf(reg.SetSave("name", "APIs-guru Registry"))
		ErrFatalf(reg.SetSave("description", "xRegistry view of github.com/APIs-guru/openapi-directory"))
		ErrFatalf(reg.SetSave("documentation", "https://github.com/duglin/xreg-github"))
		ErrFatalf(reg.Refresh())
		// log.VPrintf(3, "New registry:\n%#v", reg)

		// TODO Support "model" being part of the Registry struct above
	}

	g, err := reg.Model.AddGroupModel("apiproviders", "apiprovider")
	ErrFatalf(err)
	r, err := g.AddResourceModel("apis", "api", 2, true, true, true)
	_, err = r.AddAttr("format", registry.STRING)
	ErrFatalf(err)

	iter := 0

	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("Error getting next tar entry: %s", err)
		}

		// Skip non-regular files (and dirs)
		if header.Typeflag > '9' || header.Typeflag == tar.TypeDir {
			continue
		}

		i := 0
		// Skip files not under the APIs dir
		if i = strings.Index(header.Name, "/APIs/"); i < 0 {
			continue
		}

		// Just a subset for now
		if strings.Index(header.Name, "/docker.com/") < 0 &&
			strings.Index(header.Name, "/adobe.com/") < 0 &&
			strings.Index(header.Name, "/fec.gov/") < 0 &&
			strings.Index(header.Name, "/apiz.ebay.com/") < 0 {
			continue
		}

		parts := strings.Split(strings.Trim(header.Name[i+6:], "/"), "/")
		// org/service/version/file
		// org/version/file

		group, err := reg.FindGroup("apiproviders", parts[0], false)
		ErrFatalf(err)

		if group == nil {
			group, err = reg.AddGroup("apiproviders", parts[0])
			ErrFatalf(err)
		}

		ErrFatalf(group.SetSave("name", group.UID))
		ErrFatalf(group.SetSave("modifiedat", time.Now().Format(time.RFC3339)))
		ErrFatalf(group.SetSave("epoch", 5))

		// group2 := reg.FindGroup("apiproviders", parts[0])
		// log.Printf("Find Group:\n%s", registry.ToJSON(group2))

		resName := "core"
		verIndex := 1
		if len(parts) == 4 {
			resName = parts[1]
			verIndex++
		}

		res, err := group.AddResource("apis", resName, "v1")
		ErrFatalf(err)

		version, err := res.FindVersion(parts[verIndex], false)
		ErrFatalf(err)
		if version != nil {
			log.Fatalf("Have more than one file per version: %s\n", header.Name)
		}

		buf := &bytes.Buffer{}
		io.Copy(buf, reader)
		version, err = res.AddVersion(parts[verIndex])
		ErrFatalf(err)
		ErrFatalf(version.SetSave("name", parts[verIndex+1]))
		ErrFatalf(version.SetSave("format", "openapi/3.0.6"))

		// Don't upload the file contents into the registry. Instead just
		// give the registry a URL to it and ask it to server it via proxy.
		// We could have also just set the resourceURI to the file but
		// I wanted the URL to the file to be the registry and not github

		base := "https://raw.githubusercontent.com/APIs-guru/" +
			"openapi-directory/main/APIs/"
		switch iter % 3 {
		case 0:
			ErrFatalf(version.SetSave("#resource", buf.Bytes()))
		case 1:
			ErrFatalf(version.SetSave("#resourceURL", base+header.Name[i+6:]))
		case 2:
			ErrFatalf(version.SetSave("#resourceProxyURL", base+header.Name[i+6:]))
		}
		iter++
	}

	ErrFatalf(reg.Model.Verify())
	reg.Commit()
	return reg
}

func LoadDirsSample(reg *registry.Registry) *registry.Registry {
	var err error
	log.VPrintf(1, "Loading registry '%s'", "TestRegistry")
	if reg == nil {
		reg, err = registry.FindRegistry(nil, "TestRegistry")
		ErrFatalf(err)
		if reg != nil {
			reg.Rollback()
			return reg
		}

		reg, err = registry.NewRegistry(nil, "TestRegistry")
		ErrFatalf(err, "Error creating new registry: %s", err)
		defer reg.Rollback()

		ErrFatalf(reg.SetSave("#baseURL", "http://soaphub.org:8585/"))
		ErrFatalf(reg.SetSave("name", "Test Registry"))
		ErrFatalf(reg.SetSave("description", "A test reg"))
		ErrFatalf(reg.SetSave("documentation", "https://github.com/duglin/xreg-github"))

		ErrFatalf(reg.SetSave("labels.stage", "prod"))

		_, err = reg.Model.AddAttr("bool1", registry.BOOLEAN)
		ErrFatalf(err)
		_, err = reg.Model.AddAttr("int1", registry.INTEGER)
		ErrFatalf(err)
		_, err = reg.Model.AddAttr("dec1", registry.DECIMAL)
		ErrFatalf(err)
		_, err = reg.Model.AddAttr("str1", registry.STRING)
		ErrFatalf(err)
		_, err = reg.Model.AddAttrMap("map1", registry.NewItemType(registry.STRING))
		ErrFatalf(err)
		_, err = reg.Model.AddAttrArray("arr1", registry.NewItemType(registry.STRING))
		ErrFatalf(err)

		_, err = reg.Model.AddAttrMap("emptymap", registry.NewItemType(registry.STRING))
		ErrFatalf(err)
		_, err = reg.Model.AddAttrArray("emptyarr", registry.NewItemType(registry.STRING))
		ErrFatalf(err)
		_, err = reg.Model.AddAttrObj("emptyobj")
		ErrFatalf(err)

		item := registry.NewItemObject()
		_, err = item.AddAttr("inint", registry.INTEGER)
		ErrFatalf(err)
		_, err = reg.Model.AddAttrMap("mapobj", item)
		ErrFatalf(err)

		_, err = reg.Model.AddAttrArray("arrmap",
			registry.NewItemMap(
				registry.NewItemType(registry.STRING)))
		ErrFatalf(err)

		ErrFatalf(reg.SetSave("bool1", true))
		ErrFatalf(reg.SetSave("int1", 1))
		ErrFatalf(reg.SetSave("dec1", 1.1))
		ErrFatalf(reg.SetSave("str1", "hi"))
		ErrFatalf(reg.SetSave("map1.k1", "v1"))

		ErrFatalf(reg.SetSave("emptymap", map[string]int{}))
		ErrFatalf(reg.SetSave("emptyarr", []int{}))
		ErrFatalf(reg.SetSave("emptyobj", map[string]any{})) // struct{}{}))

		ErrFatalf(reg.SetSave("arr1[0]", "arr1-value"))
		ErrFatalf(reg.SetSave("mapobj.mapkey.inint", 5))
		ErrFatalf(reg.SetSave("mapobj['cool.key'].inint", 666))
		ErrFatalf(reg.SetSave("arrmap[1].key1", "arrmapk1-value"))
	}

	gm, err := reg.Model.AddGroupModel("dirs", "dir")
	ErrFatalf(err)
	rm, err := gm.AddResourceModel("files", "file", 2, true, true, true)
	_, err = rm.AddAttr("rext", registry.STRING)
	ErrFatalf(err)
	rm, err = gm.AddResourceModel("datas", "data", 2, true, true, false)
	ErrFatalf(err)
	_, err = rm.AddAttr("*", registry.STRING)
	ErrFatalf(err)

	ErrFatalf(reg.Model.Verify())

	g, err := reg.AddGroup("dirs", "dir1")
	ErrFatalf(err)
	ErrFatalf(g.SetSave("labels.private", "true"))
	r, err := g.AddResource("files", "f1", "v1")
	ErrFatalf(err)
	ErrFatalf(g.SetSave("labels.private", "true"))
	_, err = r.AddVersion("v2")
	ErrFatalf(err)
	ErrFatalf(r.SetSave("labels.stage", "dev"))
	ErrFatalf(r.SetSave("labels.none", ""))
	ErrFatalf(r.SetSave("rext", "a string"))

	_, err = g.AddResource("datas", "d1", "v1")

	reg.Commit()
	return reg
}

func LoadEndpointsSample(reg *registry.Registry) *registry.Registry {
	var err error
	log.VPrintf(1, "Loading registry '%s'", "Endpoints")
	if reg == nil {
		reg, err = registry.FindRegistry(nil, "Endpoints")
		ErrFatalf(err)
		if reg != nil {
			reg.Rollback()
			return reg
		}

		reg, err = registry.NewRegistry(nil, "Endpoints")
		ErrFatalf(err, "Error creating new registry: %s", err)
		defer reg.Rollback()

		ErrFatalf(reg.SetSave("#baseURL", "http://soaphub.org:8585/"))
		ErrFatalf(reg.SetSave("name", "Endpoints Registry"))
		ErrFatalf(reg.SetSave("description", "An impl of the endpoints spec"))
		ErrFatalf(reg.SetSave("documentation", "https://github.com/duglin/xreg-github"))
	}

	specPath := os.Getenv("XR_SPEC")
	if specPath == "" {
		specPath = "https://raw.githubusercontent.com/xregistry/spec/main"
	}
	fn := specPath + "/endpoint/model.json"
	err = reg.LoadModelFromFile(fn)
	ErrFatalf(err)

	/*
		ep, err := reg.Model.AddGroupModel("endpoints", "endpoint")
		ErrFatalf(err)
		attr, err := ep.AddAttr("usage", registry.STRING)
		ErrFatalf(err)
		// TODO make these required
		// attr.ClientRequired = true
		// attr.ServerRequired = true
		_, err = ep.AddAttr("origin", registry.URI)
		ErrFatalf(err)
		_, err = ep.AddAttr("channel", registry.STRING)
		ErrFatalf(err)
		attr, err = ep.AddAttrObj("deprecated")
		ErrFatalf(err)
		_, err = attr.AddAttr("effective", registry.TIMESTAMP)
		ErrFatalf(err)
		_, err = attr.AddAttr("removal", registry.TIMESTAMP)
		ErrFatalf(err)
		_, err = attr.AddAttr("alternative", registry.URL)
		ErrFatalf(err)
		_, err = attr.AddAttr("docs", registry.URL)
		ErrFatalf(err)

		config, err := attr.AddAttrObj("config")
		ErrFatalf(err)
		_, err = config.AddAttr("protocol", registry.STRING)
		ErrFatalf(err)
		obj, err := config.AddAttrMap("endpoints", registry.NewItemObject())
		ErrFatalf(err)
		obj.Item.SetItem(registry.NewItem())
		_, err = obj.Item.Item.AddAttr("*", registry.ANY)
		ErrFatalf(err)

		auth, err := config.AddAttrObj("authorization")
		ErrFatalf(err)
		attr, err = auth.AddAttr("type", registry.STRING)
		ErrFatalf(err)
		attr, err = auth.AddAttr("resourceurl", registry.STRING)
		ErrFatalf(err)
		attr, err = auth.AddAttr("authorityurl", registry.STRING)
		ErrFatalf(err)
		attr, err = auth.AddAttrArray("grant_types", registry.NewItemType(registry.STRING))
		ErrFatalf(err)

		_, err = config.AddAttr("strict", registry.BOOLEAN)
		ErrFatalf(err)

		_, err = config.AddAttrMap("options", registry.NewItemType(registry.ANY))
		ErrFatalf(err)

		_, err = ep.AddResourceModel("definitions", "definition", 2, true, true, true)
		ErrFatalf(err)
	*/

	// End of model

	g, err := reg.AddGroupWithObject("endpoints", "e1", registry.Object{
		"usage": "producer",
	}, false)
	ErrFatalf(err)
	ErrFatalf(g.SetSave("name", "end1"))
	ErrFatalf(g.SetSave("epoch", 1))
	ErrFatalf(g.SetSave("labels.stage", "dev"))
	ErrFatalf(g.SetSave("labels.stale", "true"))

	r, err := g.AddResource("messages", "created", "v1")
	ErrFatalf(err)
	v, err := r.FindVersion("v1", false)
	ErrFatalf(err)
	ErrFatalf(v.SetSave("name", "blobCreated"))
	ErrFatalf(v.SetSave("epoch", 2))

	v, err = r.AddVersion("v2")
	ErrFatalf(err)
	ErrFatalf(v.SetSave("name", "blobCreated"))
	ErrFatalf(v.SetSave("epoch", 4))
	ErrFatalf(r.SetDefault(v))

	r, err = g.AddResource("messages", "deleted", "v1.0")
	ErrFatalf(err)
	v, err = r.FindVersion("v1.0", false)
	ErrFatalf(err)
	ErrFatalf(v.SetSave("name", "blobDeleted"))
	ErrFatalf(v.SetSave("epoch", 3))

	g, err = reg.AddGroupWithObject("endpoints", "e2", registry.Object{
		"usage": "consumer",
	}, false)
	ErrFatalf(err)
	ErrFatalf(g.SetSave("name", "end1"))
	ErrFatalf(g.SetSave("epoch", 1))

	ErrFatalf(reg.Model.Verify())
	reg.Commit()
	return reg
}

func LoadMessagesSample(reg *registry.Registry) *registry.Registry {
	var err error
	log.VPrintf(1, "Loading registry '%s'", "Messages")
	if reg == nil {
		reg, err = registry.FindRegistry(nil, "Messages")
		ErrFatalf(err)
		if reg != nil {
			reg.Rollback()
			return reg
		}

		reg, err = registry.NewRegistry(nil, "Messages")
		ErrFatalf(err, "Error creating new registry: %s", err)
		defer reg.Rollback()

		reg.SetSave("#baseURL", "http://soaphub.org:8585/")
		reg.SetSave("name", "Messages Registry")
		reg.SetSave("description", "An impl of the sages spec")
		reg.SetSave("documentation", "https://github.com/duglin/xreg-github")
	}

	specPath := os.Getenv("XR_SPEC")
	if specPath == "" {
		specPath = "https://raw.githubusercontent.com/xregistry/spec/main"
	}
	fn := specPath + "/message/model.json"
	err = reg.LoadModelFromFile(fn)
	ErrFatalf(err)

	/*
		msgs, _ := reg.Model.AddGroupModel("messagegroups", "messagegroup")
		msgs.AddAttr("binding", registry.STRING)

		msg, _ := msgs.AddResourceModel("messages", "message", 1, true, true, false)

		// Modify core attribute
		attr, _ := msg.AddAttr("format", registry.STRING)
		attr.ClientRequired = true
		attr.ServerRequired = true

		msg.AddAttr("basedefinitionurl", registry.URL)

		meta, _ := msg.AddAttrObj("metadata")
		meta.AddAttr("required", registry.BOOLEAN)
		meta.AddAttr("description", registry.STRING)
		meta.AddAttr("value", registry.ANY)
		meta.AddAttr("type", registry.STRING)
		meta.AddAttr("specurl", registry.URL)

		obj := registry.NewItemObject()
		meta.AddAttrMap("attributes", obj)
		obj.AddAttr("type", registry.STRING)
		obj.AddAttr("value", registry.ANY)
		obj.AddAttr("required", registry.BOOLEAN)

		meta.AddAttr("binding", registry.STRING)
		meta.AddAttrMap("message", registry.NewItemType(registry.ANY))

		meta.AddAttr("schemaformat", registry.STRING)
		meta.AddAttr("schema", registry.ANY)
		meta.AddAttr("schemaurl", registry.URL)

		// End of model
	*/

	ErrFatalf(reg.Model.Verify())
	reg.Commit()
	return reg
}

func LoadSchemasSample(reg *registry.Registry) *registry.Registry {
	var err error
	log.VPrintf(1, "Loading registry '%s'", "Schemas")
	if reg == nil {
		reg, err = registry.FindRegistry(nil, "Schemas")
		ErrFatalf(err)
		if reg != nil {
			reg.Rollback()
			return reg
		}

		reg, err = registry.NewRegistry(nil, "Schemas")
		ErrFatalf(err, "Error creating new registry: %s", err)
		defer reg.Rollback()

		reg.SetSave("#baseURL", "http://soaphub.org:8585/")
		reg.SetSave("name", "Schemas Registry")
		reg.SetSave("description", "An impl of the schemas spec")
		reg.SetSave("documentation", "https://github.com/duglin/xreg-github")
	}

	msgs, _ := reg.Model.AddGroupModel("schemagroups", "schemagroup")
	msgs.AddResourceModel("schemas", "schema", 0, true, true, true)

	// End of model

	ErrFatalf(reg.Model.Verify())
	reg.Commit()
	return reg
}

func LoadLargeSample(reg *registry.Registry) *registry.Registry {
	var err error
	start := time.Now()
	log.VPrintf(1, "Loading registry '%s'...", "Large")
	if reg == nil {
		reg, err = registry.FindRegistry(nil, "Large")
		ErrFatalf(err)
		if reg != nil {
			reg.Rollback()
			return reg
		}

		reg, err = registry.NewRegistry(nil, "Large")
		ErrFatalf(err, "Error creating new registry: %s", err)
		defer reg.Rollback()

		reg.SetSave("#baseURL", "http://soaphub.org:8585/")
		reg.SetSave("name", "Large Registry")
		reg.SetSave("description", "A large Registry")
		reg.SetSave("documentation", "https://github.com/duglin/xreg-github")
	}

	gm, _ := reg.Model.AddGroupModel("dirs", "dir")
	gm.AddResourceModel("files", "file", 0, true, true, true)

	maxD, maxF, maxV := 10, 150, 5
	dirs, files, vers := 0, 0, 0
	for dcount := 0; dcount < maxD; dcount++ {
		dName := fmt.Sprintf("dir%d", dcount)
		d, err := reg.AddGroup("dirs", dName)
		ErrFatalf(err)
		dirs++
		for fcount := 0; fcount < maxF; fcount++ {
			fName := fmt.Sprintf("file%d", fcount)
			f, err := d.AddResource("files", fName, "v0")
			ErrFatalf(err)
			files++
			vers++
			for vcount := 1; vcount < maxV; vcount++ {
				_, err = f.AddVersion(fmt.Sprintf("v%d", vcount))
				vers++
				ErrFatalf(err)
				ErrFatalf(reg.Commit())
			}
		}
	}

	// End of model

	ErrFatalf(reg.Model.Verify())
	reg.Commit()
	dur := time.Now().Sub(start).Round(time.Second)
	log.VPrintf(1, "Done loading registry '%s' (time: %s)", "Large", dur)
	log.VPrintf(1, "Dirs: %d  Files: %d  Versions: %d", dirs, files, vers)
	return reg
}

func LoadDocStore(reg *registry.Registry) *registry.Registry {
	var err error
	log.VPrintf(1, "Loading registry '%s'", "DocStore")
	if reg == nil {
		reg, err = registry.FindRegistry(nil, "DocStore")
		ErrFatalf(err)
		if reg != nil {
			reg.Rollback()
			return reg
		}

		reg, err = registry.NewRegistry(nil, "DocStore")
		ErrFatalf(err, "Error creating new registry: %s", err)
		defer reg.Rollback()

		reg.SetSave("#baseURL", "http://soaphub.org:8585/")
		reg.SetSave("name", "DocStore Registry")
		reg.SetSave("description", "A doc store Registry")
		reg.SetSave("documentation", "https://github.com/duglin/xreg-github")
	}

	gm, _ := reg.Model.AddGroupModel("documents", "document")
	gm.AddResourceModel("formats", "format", 0, true, true, true)

	g, _ := reg.AddGroup("documents", "mydoc1")
	g.SetSave("labels.group", "g1")

	r, _ := g.AddResource("formats", "json", "v1")
	r.SetSave("contenttype", "application/json")
	r.SetSave("format", `{"prop": "A document 1"}`)

	r, _ = g.AddResource("formats", "xml", "v1")
	r.SetSave("contenttype", "application/xml")
	r.SetSave("format", `<elem title="A document 1"/>`)

	g, _ = reg.AddGroup("documents", "mydoc2")

	r, _ = g.AddResource("formats", "json", "v1")
	r.SetSave("contenttype", "application/json")
	r.SetSave("format", `{"prop": "A document 2"}`)

	r, _ = g.AddResource("formats", "xml", "v1")
	r.SetSave("contenttype", "application/xml")
	r.SetSave("format", `<elem title="A document 2"/>`)

	// End of model

	ErrFatalf(reg.Model.Verify())
	reg.Commit()
	return reg
}

func LoadOrdSample(reg *registry.Registry) *registry.Registry {
	var err error
	log.VPrintf(1, "Loading registry '%s'", "sap.foo registry")
	if reg == nil {
		reg, err = registry.FindRegistry(nil, "SapFooRegistry")
		ErrFatalf(err)

		if reg != nil {
			reg.Rollback()
			return reg
		}

		reg, err = registry.NewRegistry(nil, "SapFooRegistry")
		ErrFatalf(err, "Error creating new registry: %s", err)

		defer reg.Rollback()

		// registry root attributes + ORD mandatory attributes; have to be lower case.
		ErrFatalf(reg.SetSave("specversion", "0.5"))
		ErrFatalf(reg.SetSave("id", "SapFooRegistry"))
		ErrFatalf(reg.SetSave("description", "Example based on ORD Reference App"))

		_, err = reg.Model.AddAttr("openresourcediscovery", registry.STRING)
		ErrFatalf(reg.SetSave("openresourcediscovery", "1.9"))
		ErrFatalf(err)

		_, err = reg.Model.AddAttr("policylevel", registry.STRING)
		ErrFatalf(reg.SetSave("policylevel", "sap:core:v1"))
		ErrFatalf(err)
	}

	// adding group(group itself and model) products
	gmProducts, err := reg.Model.AddGroupModel("products", "product")
	ErrFatalf(err)

	gProduct, err := reg.AddGroup("products", "sap.foo:product:ord-reference-app:v0")
	ErrFatalf(err)

	// products(groups) attributes
	_, err = gmProducts.AddAttr("*", registry.STRING)
	ErrFatalf(gProduct.SetSave("ordid", "sap.foo:product:ord-reference-app:v0"))
	ErrFatalf(gProduct.SetSave("title", "ORD Reference App"))
	ErrFatalf(gProduct.SetSave("vendor", "sap:vendor:SAP:"))
	ErrFatalf(gProduct.SetSave("shortdescription", "Open Resource Discovery Reference Application"))
	ErrFatalf(err)

	// adding group(group itself and model) packages
	gmPackages, err := reg.Model.AddGroupModel("packages", "package")
	ErrFatalf(err)
	gPackage, err := reg.AddGroup("packages", "sap.foo:package:ord-reference-app:v0")
	ErrFatalf(err)

	// packages(groups) attributes
	_, err = gmPackages.AddAttr("ordid", registry.STRING)
	ErrFatalf(gPackage.SetSave("ordid", "sap.foo:package:ord-reference-app:v0"))
	ErrFatalf(err)

	_, err = gmPackages.AddAttr("title", registry.STRING)
	ErrFatalf(gPackage.SetSave("title", "Open Resource Discovery Reference Application"))
	ErrFatalf(err)

	_, err = gmPackages.AddAttr("shortdescription", registry.STRING)
	ErrFatalf(gPackage.SetSave("shortdescription", "This is a reference application for the Open Resource Discovery standard"))
	ErrFatalf(err)

	_, err = gmPackages.AddAttr("description", registry.STRING)
	ErrFatalf(gPackage.SetSave("description", "This reference application demonstrates how Open Resource Discovery (ORD) can be implemented, demonstrating different resources and discovery aspects"))
	ErrFatalf(err)

	_, err = gmPackages.AddAttr("version", registry.STRING)
	ErrFatalf(gPackage.SetSave("version", "0.3.0"))
	ErrFatalf(err)

	_, err = gmPackages.AddAttr("policylevel", registry.STRING)
	ErrFatalf(gPackage.SetSave("policylevel", "sap:core:v1"))
	ErrFatalf(err)

	_, err = gmPackages.AddAttrArray("partofproducts", registry.NewItemType(registry.STRING))
	ErrFatalf(gPackage.SetSave("partofproducts[0]", "sap.foo:product:ord-reference-app:"))
	ErrFatalf(err)

	_, err = gmPackages.AddAttr("vendor", registry.STRING)
	ErrFatalf(gPackage.SetSave("vendor", "sap:vendor:SAP:"))
	ErrFatalf(err)

	_, err = gmPackages.AddAttrArray("tags", registry.NewItemType(registry.STRING))
	ErrFatalf(gPackage.SetSave("tags[0]", "reference application"))
	ErrFatalf(err)

	// NOTE: "labels" in ORD specification is of type array<string>, whilst in xRegistry it is string !
	_, err = gmPackages.AddAttrMap("labels", registry.NewItemArray(registry.NewItemType(registry.STRING)))
	ErrFatalf(gPackage.SetSave("labels.customLabel[0]", "labels are more flexible than tags as you can define your own keys"))
	ErrFatalf(err)

	_, err = gmPackages.AddAttrMap("documentationlabels", registry.NewItemArray(registry.NewItemType(registry.STRING)))
	// NOTE: In original ORD document, the key has value "Some Aspect"(with space!) which is not allowed
	ErrFatalf(gPackage.SetSave("documentationlabels.SomeAspect[0]", "Markdown Documentation [with links](#)"))
	ErrFatalf(gPackage.SetSave("documentationlabels.SomeAspect[1]", "With multiple values"))
	ErrFatalf(err)

	// adding group(group itself and model) consumptionbundles
	gmConsumptionBundles, err := reg.Model.AddGroupModel("consumptionbundles", "consumptionbundle")
	ErrFatalf(err)
	gConsumptionBundle, err := reg.AddGroup("consumptionbundles", "sap.foo:consumptionBundle:noAuth:v1")
	ErrFatalf(err)
	// // consumptionBundles(groups) attributes
	_, err = gmConsumptionBundles.AddAttr("*", registry.STRING)
	ErrFatalf(err)
	ErrFatalf(gConsumptionBundle.SetSave("ordid", "sap.foo:consumptionBundle:noAuth:v1"))
	ErrFatalf(gConsumptionBundle.SetSave("shortdescription", "Bundle of unprotected resources"))
	ErrFatalf(gConsumptionBundle.SetSave("description", "This Consumption Bundle contains all resources of the reference app which are unprotected and do not require authentication"))
	ErrFatalf(gConsumptionBundle.SetSave("version", "1.0.0"))
	// QUESTION: There is already field modifiedat automatically attached, is the lastUpdate redundant then?
	ErrFatalf(gConsumptionBundle.SetSave("lastUpdate", "2022-12-19T15:47:04+00:00"))

	// adding group(group itself and model) apiResources
	gmApiResources, err := reg.Model.AddGroupModel("apiresources", "apiresource")
	ErrFatalf(err)
	gApiResource, err := reg.AddGroup("apiresources", "sap.foo:apiResource:astronomy:v1")
	ErrFatalf(err)

	// apiResources(groups) attributes
	_, err = gmApiResources.AddAttr("ordid", registry.STRING)
	ErrFatalf(gApiResource.SetSave("ordid", "sap.foo:apiResource:astronomy:v1"))
	ErrFatalf(err)
	_, err = gmApiResources.AddAttr("title", registry.STRING)
	ErrFatalf(gApiResource.SetSave("title", "Astronomy API"))
	ErrFatalf(err)
	_, err = gmApiResources.AddAttr("shortdescription", registry.STRING)
	ErrFatalf(gApiResource.SetSave("shortdescription", "The API allows you to discover..."))
	ErrFatalf(err)
	_, err = gmApiResources.AddAttr("description", registry.STRING)
	ErrFatalf(gApiResource.SetSave("description", "A longer description of this API with **markdown** \n## headers\n etc..."))
	ErrFatalf(err)
	_, err = gmApiResources.AddAttr("version", registry.STRING)
	ErrFatalf(gApiResource.SetSave("version", "1.0.3"))
	ErrFatalf(err)
	_, err = gmApiResources.AddAttr("visibility", registry.STRING)
	ErrFatalf(gApiResource.SetSave("visibility", "public"))
	ErrFatalf(err)
	_, err = gmApiResources.AddAttr("releasestatus", registry.STRING)
	ErrFatalf(gApiResource.SetSave("releasestatus", "active"))
	ErrFatalf(err)
	_, err = gmApiResources.AddAttr("systeminstanceaware", registry.BOOLEAN)
	ErrFatalf(gApiResource.SetSave("systeminstanceaware", false))
	ErrFatalf(err)
	_, err = gmApiResources.AddAttr("policylevel", registry.STRING)
	ErrFatalf(gApiResource.SetSave("policylevel", "custom"))
	ErrFatalf(err)
	_, err = gmApiResources.AddAttr("custompolicylevel", registry.STRING)
	ErrFatalf(gApiResource.SetSave("custompolicylevel", "sap.foo:custom:v1"))
	ErrFatalf(err)
	_, err = gmApiResources.AddAttr("partofpackage", registry.STRING)
	ErrFatalf(gApiResource.SetSave("partofpackage", "sap.foo:package:ord-reference-app:v1"))
	ErrFatalf(err)
	_, err = gmApiResources.AddAttrArray("partofconsumptionbundles", registry.NewItemMap(registry.NewItemType(registry.STRING)))
	ErrFatalf(gApiResource.SetSave("partofconsumptionbundles[0].ordId", "sap.foo:consumptionBundle:noAuth:v1"))
	ErrFatalf(err)
	_, err = gmApiResources.AddAttrArray("partofgroups", registry.NewItemType(registry.STRING))
	ErrFatalf(gApiResource.SetSave("partofgroups[0]", "sap.foo:groupTypeAbc:sap.foo:groupAssignmentValue"))
	ErrFatalf(err)
	_, err = gmApiResources.AddAttr("apiprotocol", registry.STRING)
	ErrFatalf(gApiResource.SetSave("apiprotocol", "rest"))
	ErrFatalf(err)
	_, err = gmApiResources.AddAttrArray("apiresourcelinks", registry.NewItemMap(registry.NewItemType(registry.STRING)))
	ErrFatalf(gApiResource.SetSave("apiresourcelinks[0].type", "api-documentation"))
	ErrFatalf(gApiResource.SetSave("apiresourcelinks[0].url", "/swagger-ui.html?urls.primaryName=Astronomy%20V1%20API"))
	ErrFatalf(err)
	_, err = gmApiResources.AddAttrArray("entrypoints", registry.NewItemType(registry.STRING))
	ErrFatalf(gApiResource.SetSave("entrypoints[0]", "/astronomy/v1"))
	ErrFatalf(err)
	_, err = gmApiResources.AddAttrMap("extensible", registry.NewItemType(registry.STRING))
	ErrFatalf(gApiResource.SetSave("extensible.supported", "no"))
	ErrFatalf(err)
	// adding resource to the apiResources group with name resourceDefinitions
	rmApiResource, err := gmApiResources.AddResourceModel("resourcedefinitions", "resourcedefinition", 2, true, true, false)
	ErrFatalf(err)
	rd, err := gApiResource.AddResource("resourcedefinitions", "*", "v1")
	ErrFatalf(err)

	// apiResources[0].resourceDefinitions(resource)
	_, err = rmApiResource.AddAttr("type", registry.STRING)
	ErrFatalf(rd.SetSave("type", "openapi-v3"))
	ErrFatalf(err)
	_, err = rmApiResource.AddAttr("mediatype", registry.STRING)
	ErrFatalf(rd.SetSave("mediatype", "application/json"))
	ErrFatalf(err)
	_, err = rmApiResource.AddAttr("url", registry.STRING)
	ErrFatalf(rd.SetSave("url", "/ord/metadata/astronomy-v1.oas3.json"))
	ErrFatalf(err)
	_, err = rmApiResource.AddAttrArray("accessstrategies", registry.NewItemMap(registry.NewItemType(registry.STRING)))
	ErrFatalf(rd.SetSave("accessstrategies[0].type", "open"))
	ErrFatalf(err)
	// adding group(group itself and model) eventResources
	gmEventResources, err := reg.Model.AddGroupModel("eventresources", "eventresource")
	ErrFatalf(err)
	gEventResource1, err := reg.AddGroup("eventresources", "sap.foo:eventResource:ExampleEventResource:v1")
	ErrFatalf(err)
	gEventResource2, err := reg.AddGroup("eventresources", "sap.foo:eventResource:BillingDocumentEvents:v1")
	ErrFatalf(err)

	// eventResources(groups) attributes
	_, err = gmEventResources.AddAttr("ordid", registry.STRING)
	ErrFatalf(err)
	_, err = gmEventResources.AddAttr("title", registry.STRING)
	ErrFatalf(err)
	_, err = gmEventResources.AddAttr("shortdescription", registry.STRING)
	ErrFatalf(err)
	_, err = gmEventResources.AddAttr("description", registry.STRING)
	ErrFatalf(err)
	_, err = gmEventResources.AddAttr("version", registry.STRING)
	ErrFatalf(err)
	_, err = gmEventResources.AddAttr("lastupdate", registry.STRING) // QUESTION: Is this field redundant having in mind "modifiedat"?
	ErrFatalf(err)
	_, err = gmEventResources.AddAttr("releasestatus", registry.STRING)
	ErrFatalf(err)
	_, err = gmEventResources.AddAttr("partofpackage", registry.STRING)
	ErrFatalf(err)
	_, err = gmEventResources.AddAttr("visibility", registry.STRING)
	ErrFatalf(err)
	_, err = gmEventResources.AddAttrMap("extensible", registry.NewItemType(registry.STRING))
	ErrFatalf(err)
	// adding resource to the eventResources groups with name resourceDefinitions
	rmEventResource, err := gmEventResources.AddResourceModel("resourcedefinitions", "resourcedefinition", 2, true, true, false)
	ErrFatalf(err)

	_, err = rmEventResource.AddAttr("type", registry.STRING)
	ErrFatalf(err)
	_, err = rmEventResource.AddAttr("mediatype", registry.STRING)
	ErrFatalf(err)
	_, err = rmEventResource.AddAttr("url", registry.STRING)
	ErrFatalf(err)
	_, err = rmEventResource.AddAttrArray("accessstrategies", registry.NewItemMap(registry.NewItemType(registry.STRING)))
	ErrFatalf(err)
	// eventresources.eventResource1 group
	ErrFatalf(gEventResource1.SetSave("ordid", "sap.foo:eventResource:ExampleEventResource:v1"))
	ErrFatalf(gEventResource1.SetSave("title", "Event Example"))
	ErrFatalf(gEventResource1.SetSave("shortdescription", "Simple Event Example"))
	ErrFatalf(gEventResource1.SetSave("description", "Example long description"))
	ErrFatalf(gEventResource1.SetSave("version", "1.2.1"))
	ErrFatalf(gEventResource1.SetSave("lastupdate", "2022-12-19T15:47:04+00:00"))
	ErrFatalf(gEventResource1.SetSave("releasestatus", "beta"))
	ErrFatalf(gEventResource1.SetSave("partofpackage", "sap.foo:package:SomePackage:v1"))
	ErrFatalf(gEventResource1.SetSave("visibility", "public"))
	ErrFatalf(gEventResource1.SetSave("extensible.supported", "no"))

	rde1, err := gEventResource1.AddResource("resourcedefinitions", "*", "v1")
	ErrFatalf(err)
	ErrFatalf(rde1.SetSave("type", "asyncapi-v2"))
	ErrFatalf(rde1.SetSave("mediatype", "application/json"))
	ErrFatalf(rde1.SetSave("url", "/some/path/asyncApi2.json"))
	ErrFatalf(rde1.SetSave("accessstrategies[0].type", "open"))

	// eventresources.eventResource2 group
	ErrFatalf(gEventResource2.SetSave("ordid", "sap.foo:eventResource:BillingDocumentEvents:v1"))
	ErrFatalf(gEventResource2.SetSave("title", "Billing Document Events"))
	ErrFatalf(gEventResource2.SetSave("shortdescription", "Informs a remote system about created, changed, and canceled billing documents"))
	ErrFatalf(gEventResource2.SetSave("description", "Billing document is an umbrella term for invoices, credit memos, debit memos, pro forma invoices, and their respective cancellation documents. The following events are available for billing document:\n      Billing document canceled\n      Billing document changed\n      Billing Document created"))
	ErrFatalf(gEventResource2.SetSave("version", "1.0.0"))
	ErrFatalf(gEventResource2.SetSave("lastupdate", "2022-12-19T15:47:04+00:00"))
	ErrFatalf(gEventResource2.SetSave("releasestatus", "active"))
	ErrFatalf(gEventResource2.SetSave("partofpackage", "sap.foo:package:SomePackage:v1"))
	ErrFatalf(gEventResource2.SetSave("visibility", "public"))
	ErrFatalf(gEventResource2.SetSave("extensible.supported", "no"))

	rde2, err := gEventResource2.AddResource("resourcedefinitions", "*", "v1")
	ErrFatalf(err)

	ErrFatalf(rde2.SetSave("type", "asyncapi-v2"))
	ErrFatalf(rde2.SetSave("mediatype", "application/json"))
	ErrFatalf(rde2.SetSave("url", "/api/eventCatalog.json"))
	ErrFatalf(rde2.SetSave("accessstrategies[0].type", "open"))

	// adding group(group itself and model) capabilities
	gmCapabilities, err := reg.Model.AddGroupModel("capabilities", "capability")
	gCapability, err := reg.AddGroup("capabilities", "sap.foo.bar:capability:mdi:v1")

	// capabilities(groups) attributes
	_, err = gmCapabilities.AddAttr("ordid", registry.STRING)
	ErrFatalf(gCapability.SetSave("ordid", "sap.foo.bar:capability:mdi:v1"))

	_, err = gmCapabilities.AddAttr("title", registry.STRING)
	ErrFatalf(gCapability.SetSave("title", "Master Data Integration Capability"))

	_, err = gmCapabilities.AddAttr("type", registry.STRING)
	ErrFatalf(gCapability.SetSave("type", "sap.mdo:mdi-capability:v1"))

	_, err = gmCapabilities.AddAttr("shortdescription", registry.STRING)
	ErrFatalf(gCapability.SetSave("shortdescription", "Short description of capability"))

	_, err = gmCapabilities.AddAttr("description", registry.STRING)
	ErrFatalf(gCapability.SetSave("description", "Optional, longer description"))

	_, err = gmCapabilities.AddAttr("version", registry.STRING)
	ErrFatalf(gCapability.SetSave("version", "1.0.0"))

	_, err = gmCapabilities.AddAttr("version", registry.STRING)
	ErrFatalf(gCapability.SetSave("version", "1.0.0"))

	_, err = gmCapabilities.AddAttr("lastupdate", registry.STRING)
	ErrFatalf(gCapability.SetSave("lastupdate", "2023-01-26T15:47:04+00:00"))

	_, err = gmCapabilities.AddAttr("releasestatus", registry.STRING)
	ErrFatalf(gCapability.SetSave("releasestatus", "active"))

	_, err = gmCapabilities.AddAttr("visibility", registry.STRING)
	ErrFatalf(gCapability.SetSave("visibility", "public"))

	_, err = gmCapabilities.AddAttr("partofpackage", registry.STRING)
	ErrFatalf(gCapability.SetSave("partofpackage", "sap.foo.bar:package:SomePackage:v1"))

	_, err = gmCapabilities.AddAttrArray("definitions", registry.NewItemMap(registry.NewItemType(registry.ANY)))
	ErrFatalf(gCapability.SetSave("definitions[0].type", "sap.mdo:mdi-capability-definition:v1"))
	ErrFatalf(gCapability.SetSave("definitions[0].mediaType", "application/json"))
	ErrFatalf(gCapability.SetSave("definitions[0].url", "/capabilities/foo.bar.json"))
	ErrFatalf(gCapability.SetSave("definitions[0].accessStrategies[0].type", "open"))

	ErrFatalf(err)

	// adding group(group itself and model) "groups"
	gmGroups, err := reg.Model.AddGroupModel("groups", "group")
	ErrFatalf(err)
	gGroup, err := reg.AddGroup("groups", "sap.foo:groupTypeAbc:sap.foo:groupAssignmentValue")
	ErrFatalf(err)
	// "groups"(groups) attributes
	_, err = gmGroups.AddAttr("*", registry.STRING)
	ErrFatalf(gGroup.SetSave("groupid", "sap.foo:groupTypeAbc:sap.foo:groupAssignmentValue"))
	ErrFatalf(gGroup.SetSave("grouptypeid", "sap.foo:groupTypeAbc"))
	ErrFatalf(gGroup.SetSave("title", "Title of group assignment / instance"))

	ErrFatalf(err)

	// adding group(group itself and model) "groupTypes"
	gmGroupTypes, err := reg.Model.AddGroupModel("grouptypes", "grouptype")
	ErrFatalf(err)
	gGroupType, err := reg.AddGroup("grouptypes", "sap.foo:groupTypeAbc")
	ErrFatalf(err)
	// "grouptypes"(groups) attributes
	_, err = gmGroupTypes.AddAttr("*", registry.STRING)
	ErrFatalf(gGroupType.SetSave("grouptypeid", "sap.foo:groupTypeAbc"))
	ErrFatalf(gGroupType.SetSave("title", "Title of group type"))

	ErrFatalf(err)

	// adding group(group itself and model) "tombstones"
	gmTombstones, err := reg.Model.AddGroupModel("tombstones", "tombstone")
	ErrFatalf(err)
	gTombstone, err := reg.AddGroup("tombstones", "sap.foo:apiResource:astronomy:v0")
	ErrFatalf(err)
	// "grouptypes"(groups) attributes
	_, err = gmTombstones.AddAttr("*", registry.STRING)
	ErrFatalf(gTombstone.SetSave("ordid", "sap.foo:apiResource:astronomy:v0"))
	ErrFatalf(gTombstone.SetSave("removalDate", "2020-12-02T14:12:59Z"))

	ErrFatalf(err)

	ErrFatalf(reg.Model.Verify())

	reg.Commit()
	return reg
}

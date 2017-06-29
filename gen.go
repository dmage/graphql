package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"strings"

	"github.com/dmage/graphql/pkg/schema"
	"github.com/dmage/graphql/pkg/schema/typekind"
)

type FieldConfig struct {
	Name   string
	Import string
	Type   string
}

type TypeConfig struct {
	Name   string
	Import string
	Type   string
	Fields map[string]FieldConfig

	// File into which the definition of a scalar should be written.
	// Should be empty string for a predeclared name and for an imported type.
	File string
}

// ScalarConfig defines a type for a scalar. The scalar will be replaced by
// a built-in type if Name is a predeclared name.
type ScalarConfig struct {
	// Name of a scalar. Optional if the scalar should not be renamed.
	Name string

	// Import path for the package with Type. May be omitted if Type is
	// a predeclared type or available in the same package as the scalar.
	Import string

	// Type of a scalar. Optional if Name is a predeclared name.
	Type string

	// File into which the definition of a scalar should be written.
	// Should be empty string for a predeclared name.
	File string
}

type Config struct {
	Types   map[string]TypeConfig
	Scalars map[string]ScalarConfig
}

type OutputFile struct {
	Package string
	Imports []string
	Chunks  []string
}

func (f *OutputFile) HasImport(s string) bool {
	for _, im := range f.Imports {
		if im == s {
			return true
		}
	}
	return false
}

func (f *OutputFile) Add(imports []string, chunk string) {
	for _, im := range imports {
		if !f.HasImport(im) {
			f.Imports = append(f.Imports, im)
		}
	}
	f.Chunks = append(f.Chunks, chunk)
}

type OutputFiles map[string]*OutputFile

func (fs OutputFiles) Get(file string, pkg string) *OutputFile {
	of, ok := fs[file]
	if ok {
		return of
	}

	of = &OutputFile{
		Package: pkg,
	}
	fs[file] = of
	return of
}

func isPredeclaredType(t string) bool {
	// https://golang.org/ref/spec#Predeclared_identifiers
	switch t {
	case "bool", "byte", "complex64", "complex128", "error", "float32", "float64",
		"int", "int8", "int16", "int32", "int64", "rune", "string",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr":
		return true
	}
	return false
}

var defaultScalars = map[string]ScalarConfig{
	"Int":     {Name: "int32"},
	"Float":   {Name: "float64"},
	"String":  {Name: "string"},
	"Boolean": {Name: "bool"},
	"ID":      {Type: "string"},
}

func getScalarConfig(config *Config, name string) ScalarConfig {
	cfg, ok := config.Scalars[name]
	if ok {
		return cfg
	}

	cfg, ok = defaultScalars[name]
	if ok {
		return cfg
	}

	return ScalarConfig{}
}

func getNameNullable(config *Config, typ schema.Type, nullable bool) string {
	prefix := ""
	if nullable {
		prefix = "*"
	}

	switch typ.Kind {
	case typekind.Scalar:
		cfg := getScalarConfig(config, *typ.Name)

		if cfg.Name != "" && isPredeclaredType(cfg.Name) {
			if cfg.Type != "" && cfg.Type != cfg.Name {
				log.Fatalf("the scalar named %q could not have the type %q", cfg.Name, cfg.Type)
			}
			return prefix + cfg.Name
		}

		if cfg.Name != "" {
			return prefix + cfg.Name
		}
	case typekind.NonNull:
		return getNameNullable(config, *typ.OfType, false)
	case typekind.List:
		return "[]" + getNameNullable(config, *typ.OfType, true)
	}
	if typ.Name == nil {
		panic(fmt.Errorf("unable to get name for type %#+v", typ))
	}
	return prefix + *typ.Name
}

func getName(config *Config, typ schema.Type) string {
	return getNameNullable(config, typ, true)
}

func getFile(config *Config, typ schema.Type) string {
	name := getNameNullable(config, typ, false)
	if isPredeclaredType(name) {
		return ""
	}

	switch typ.Kind {
	case typekind.Scalar:
		cfg := getScalarConfig(config, *typ.Name)
		if cfg.File != "" {
			return cfg.File
		}
		return "scalars.go"
	case typekind.Object:
		// ...
		return "types.go"
	case typekind.Enum:
		n := strings.ToLower(name)
		return path.Join(n, n+".go")
	case typekind.Interface:
		return "interfaces.go"
	case typekind.Union:
		return "unions.go"
	case typekind.InputObject:
		return "" // FIXME
	}
	panic(fmt.Errorf("don't know how to get file for %#+v", typ))
}

func renderComment(prefix, s string) string {
	return fmt.Sprintf("%s%s\n", prefix, strings.Replace(s, "\n", "\n"+prefix, -1))
}

func renderObject(config *Config, typ schema.Type) string {
	var buf bytes.Buffer
	if typ.Description != nil {
		buf.WriteString(renderComment("// ", *typ.Description))
	}
	name := getNameNullable(config, typ, false)
	fmt.Fprintf(&buf, "type %s struct {\n", name)
	for fieldNo, field := range typ.Fields {
		if fieldNo != 0 {
			buf.WriteString("\n")
		}
		if field.Description != nil {
			buf.WriteString(renderComment("\t// ", *field.Description))
		}
		fmt.Fprintf(&buf, "\t%s %v\n", strings.Title(field.Name), getName(config, field.Type))
	}
	buf.WriteString("}\n")
	return buf.String()
}

func renderScalar(config *Config, typ schema.Type) string {
	name := getNameNullable(config, typ, false)

	cfg := getScalarConfig(config, *typ.Name)
	if cfg.Type == "" {
		log.Fatalf("the definition for the scalar named %q could not be generated without a type", name)
	}

	var buf bytes.Buffer
	if typ.Description != nil {
		buf.WriteString(renderComment("// ", *typ.Description))
	}
	fmt.Fprintf(&buf, "type %s %s\n", name, cfg.Type)
	return buf.String()
}

func renderInterface(config *Config, typ schema.Type) string {
	name := getNameNullable(config, typ, false)

	var buf bytes.Buffer
	if typ.Description != nil {
		buf.WriteString(renderComment("// ", *typ.Description))
	}
	if len(typ.Fields) != 0 {
		fmt.Fprintf(&buf, "type %s interface{\n", name)
		for fieldNo, field := range typ.Fields {
			if fieldNo != 0 {
				buf.WriteString("\n")
			}
			if field.Description != nil {
				buf.WriteString(renderComment("\t// ", *field.Description))
			}
			fmt.Fprintf(&buf, "\t%s() %s\n", strings.Title(field.Name), getName(config, field.Type))
		}
		buf.WriteString("}\n")
	} else {
		fmt.Fprintf(&buf, "type %s interface{}\n", name)
	}
	return buf.String()
}

func renderUnion(config *Config, typ schema.Type) string {
	name := getNameNullable(config, typ, false)

	var buf bytes.Buffer
	if typ.Description != nil {
		buf.WriteString(renderComment("// ", *typ.Description))
	}
	fmt.Fprintf(&buf, "type %s interface{}\n", name)
	return buf.String()
}

func renderEnum(config *Config, typ schema.Type) string {
	name := getNameNullable(config, typ, false)

	var buf bytes.Buffer
	if typ.Description != nil {
		buf.WriteString(renderComment("// ", *typ.Description))
	}
	fmt.Fprintf(&buf, "type %s string\n", name)
	return buf.String()
}

func main() {
	f, err := os.Open("config.json")
	if err != nil {
		log.Fatal(err)
	}

	var config Config
	err = json.NewDecoder(f).Decode(&config)
	if err != nil {
		log.Fatal("decode config: ", err)
	}

	var v struct {
		Data struct {
			Schema schema.Schema `json:"__schema"`
		}
	}
	err = json.NewDecoder(os.Stdin).Decode(&v)
	if err != nil {
		log.Fatal(err)
	}

	outputFiles := make(OutputFiles)
	pkg := "fixmepkg"

	for _, typ := range v.Data.Schema.Types {
		file := getFile(&config, typ)
		if file == "" {
			continue
		}
		of := outputFiles.Get(file, pkg)

		switch typ.Kind {
		case typekind.Object:
			chunk := renderObject(&config, typ)
			of.Add(nil, chunk)
		case typekind.Scalar:
			chunk := renderScalar(&config, typ)
			of.Add(nil, chunk)
		case typekind.Enum:
			chunk := renderEnum(&config, typ)
			of.Add(nil, chunk)
		case typekind.Interface:
			chunk := renderInterface(&config, typ)
			of.Add(nil, chunk)
		case typekind.Union:
			chunk := renderUnion(&config, typ)
			of.Add(nil, chunk)
		default:
			fmt.Printf("SKIP %s %s\n", typ.Kind, *typ.Name)
		}
	}

	for file, of := range outputFiles {
		file = "./fixmepkg/" + file
		err := os.MkdirAll(path.Dir(file), 0777)
		if err != nil {
			log.Fatal(err)
		}

		f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE, 0666)
		if err != nil {
			log.Fatal(err)
		}

		fmt.Fprintf(f, "package %s\n", of.Package)
		// TODO: imports
		for _, chunk := range of.Chunks {
			f.WriteString("\n")
			f.WriteString(chunk)
		}

		f.Close()
	}
}

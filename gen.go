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

	// File into which the definition of an object should be written.
	// Should be empty for predeclared names and imported types.
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
	// Should be empty for predeclared names.
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

func getTypeConfig(config *Config, name string) TypeConfig {
	return config.Types[name]
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
	case typekind.Interface:
		prefix = ""
	}
	if typ.Name == nil {
		panic(fmt.Errorf("unable to get name for type %#+v", typ))
	}
	return prefix + *typ.Name
}

func getName(config *Config, typ schema.Type) string {
	return getNameNullable(config, typ, true)
}

func getFieldType(config *Config, typ schema.Type, field schema.Field) string {
	cfg := getTypeConfig(config, *typ.Name)
	fieldCfg := cfg.Fields[field.Name]
	if fieldCfg.Type != "" {
		return fieldCfg.Type
	}
	return getName(config, field.Type)
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
		return "enums.go"
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

func renderObject(config *Config, typ schema.Type) ([]string, string) {
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
		fieldType := getFieldType(config, typ, field)
		fmt.Fprintf(&buf, "\tfield_%s %s `json:\"%s\"`\n", field.Name, fieldType, field.Name)
	}
	buf.WriteString("}\n")
	for _, field := range typ.Fields {
		fieldType := getFieldType(config, typ, field)
		fmt.Fprintf(&buf, "\nfunc (o %s) %s() %s {\n", name, strings.Title(field.Name), fieldType)
		fmt.Fprintf(&buf, "\treturn o.field_%s\n", field.Name)
		fmt.Fprintf(&buf, "}\n")
	}
	fmt.Fprintf(&buf, "\nfunc (o *%s) UnmarshalJSON(data []byte) error {\n", name)
	fmt.Fprintf(&buf, "\tvar v struct {\n")
	for _, field := range typ.Fields {
		if field.Type.Kind == typekind.Interface {
			fmt.Fprintf(&buf, "\t\tfield_%s json.RawMessage `json:\"%s\"`\n", field.Name, field.Name)
		} else {
			fieldType := getFieldType(config, typ, field)
			fmt.Fprintf(&buf, "\t\tfield_%s %s `json:\"%s\"`\n", field.Name, fieldType, field.Name)
		}
	}
	fmt.Fprintf(&buf, "\t}\n")
	fmt.Fprintf(&buf, "\terr := json.Unmarshal(data, &v)\n")
	fmt.Fprintf(&buf, "\tif err != nil {\n")
	fmt.Fprintf(&buf, "\t\treturn err\n")
	fmt.Fprintf(&buf, "\t}\n")
	for _, field := range typ.Fields {
		if field.Type.Kind == typekind.Interface {
			fieldType := getFieldType(config, typ, field)
			fmt.Fprintf(&buf, "\to.field_%s, err = %s_UnmarshalJSON(v.field_%s)\n", field.Name, fieldType, field.Name)
			fmt.Fprintf(&buf, "\tif err != nil {\n")
			fmt.Fprintf(&buf, "\t\treturn err\n")
			fmt.Fprintf(&buf, "\t}\n")
		} else {
			fmt.Fprintf(&buf, "\to.field_%s = v.field_%s\n", field.Name, field.Name)
		}
	}
	fmt.Fprintf(&buf, "\treturn nil\n")
	fmt.Fprintf(&buf, "}\n")
	return []string{"encoding/json"}, buf.String()
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

func renderInterface(config *Config, typ schema.Type) ([]string, string) {
	name := getNameNullable(config, typ, false)

	var buf bytes.Buffer
	if typ.Description != nil {
		buf.WriteString(renderComment("// ", *typ.Description))
	}
	if len(typ.Fields) == 0 {
		fmt.Fprintf(&buf, "type %s interface{}\n", name)
	} else {
		fmt.Fprintf(&buf, "type %s interface {\n", name)
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
	}
	fmt.Fprintf(&buf, "\ntype Untyped_%s map[string]interface{}\n", name)
	for _, field := range typ.Fields {
		fmt.Fprintf(&buf, "\nfunc (m Untyped_%s) %s() %s {\n", name, strings.Title(field.Name), getName(config, field.Type))
		fmt.Fprintf(&buf, "\treturn m[%q].(%s)\n", field.Name, getName(config, field.Type))
		fmt.Fprintf(&buf, "}\n")
	}
	fmt.Fprintf(&buf, "\nfunc %s_UnmarshalJSON(data []byte) (%s, error) {\n", name, name)
	fmt.Fprintf(&buf, "\tvar t struct {\n\t\ttypename string `json:\"__typename\"`\n\t}\n")
	fmt.Fprintf(&buf, "\terr := json.Unmarshal(data, &t)\n")
	fmt.Fprintf(&buf, "\tif err != nil {\n")
	fmt.Fprintf(&buf, "\t\treturn nil, err\n")
	fmt.Fprintf(&buf, "\t}\n")
	fmt.Fprintf(&buf, "\tswitch t.typename {\n")
	for _, pt := range typ.PossibleTypes {
		fmt.Fprintf(&buf, "\tcase %q:\n", *pt.Name)
		fmt.Fprintf(&buf, "\t\tvar v %s\n", getNameNullable(config, pt, false))
		fmt.Fprintf(&buf, "\t\terr = json.Unmarshal(data, &v)\n")
		fmt.Fprintf(&buf, "\t\treturn v, err\n")
	}
	fmt.Fprintf(&buf, "\tcase \"\":\n")
	fmt.Fprintf(&buf, "\t\tvar v Untyped_%s\n", name)
	fmt.Fprintf(&buf, "\t\terr = json.Unmarshal(data, &v)\n")
	fmt.Fprintf(&buf, "\t\treturn v, err\n")
	fmt.Fprintf(&buf, "\t}\n")
	fmt.Fprintf(&buf, "\treturn nil, fmt.Errorf(\"unexpected __typename for interface %%s: %%s\", %q, t.typename)\n", name)
	fmt.Fprintf(&buf, "}\n")
	return []string{"encoding/json", "fmt"}, buf.String()
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
	if len(typ.EnumValues) > 0 {
		fmt.Fprintf(&buf, "\nconst (\n")
		for i, val := range typ.EnumValues {
			if i != 0 {
				buf.WriteString("\n")
			}
			if val.Description != nil {
				buf.WriteString(renderComment("\t// ", *val.Description))
			}
			fmt.Fprintf(&buf, "\t%s_%s %s = %q\n", name, val.Name, name, val.Name)
		}
		buf.WriteString(")\n")
	}
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
			imports, chunk := renderObject(&config, typ)
			of.Add(imports, chunk)
		case typekind.Scalar:
			chunk := renderScalar(&config, typ)
			of.Add(nil, chunk)
		case typekind.Enum:
			chunk := renderEnum(&config, typ)
			of.Add(nil, chunk)
		case typekind.Interface:
			imports, chunk := renderInterface(&config, typ)
			of.Add(imports, chunk)
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

		f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
		if err != nil {
			log.Fatal(err)
		}

		fmt.Fprintf(f, "package %s\n", of.Package)
		if len(of.Imports) > 0 {
			f.WriteString("\n")
			f.WriteString("import (\n")
			for _, im := range of.Imports {
				fmt.Fprintf(f, "\t%q\n", im)
			}
			f.WriteString(")\n")
		}
		for _, chunk := range of.Chunks {
			f.WriteString("\n")
			f.WriteString(chunk)
		}

		f.Close()
	}
}

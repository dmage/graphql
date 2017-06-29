// Package schema defines types for the schema introspection system.
//
// http://facebook.github.io/graphql/#sec-Schema-Introspection
package schema

import (
	"fmt"

	"github.com/dmage/graphql/pkg/schema/typekind"
)

type Schema struct {
	Types            []Type
	QueryType        Type
	MutationType     *Type
	SubscriptionType *Type
	//Directives       []Directive
}

type Type struct {
	Kind          typekind.TypeKind
	Name          *string
	Description   *string
	Fields        []Field
	Interfaces    []Type
	PossibleTypes []Type
	//EnumValues []EnumValue
	//InputFields []InputField
	OfType *Type
}

func (t Type) goName(nullable bool) string {
	switch t.Kind {
	case typekind.Object:
		if nullable {
			return "*" + *t.Name
		}
		return *t.Name
	case typekind.NonNull:
		return t.OfType.goName(false)
	case typekind.Scalar:
		switch *t.Name {
		case "String":
			return "string"
		case "DateTime":
			// TODO(dmage): add DateTime
			return "TODO_DateTime"
		case "Int":
			return "int"
		case "HTML":
			return "TODO_HTML"
		case "Boolean":
			return "bool"
		case "URI":
			return "TODO_URI"
		case "ID":
			return "TODO_ID"
		case "GitObjectID":
			return "TODOO_GitObjectID"
		case "GitTimestamp":
			return "TODO_GitTimestamp"
		case "X509Certificate":
			return "TODO_X509Certificate"
		}
		panic(*t.Name)
		return "SCALAR_" + *t.Name
	case typekind.List:
		return "[]" + t.OfType.GoName()
	case typekind.Union:
		return "UNION_" + *t.Name
	case typekind.Enum:
		return "ENUM_" + *t.Name
	case typekind.Interface:
		return "INTERFACE_" + *t.Name
	}
	panic(fmt.Sprintf("%+v", t))
}

func (t Type) GoName() string {
	return t.goName(true)
}

type Field struct {
	Name        string
	Description *string
	// Args []InputValue
	Type              Type
	IsDeprecated      bool
	DeprecationReason *string
}

/*
type __InputValue {
  name: String!
  description: String
  type: __Type!
  defaultValue: String
}
*/

/*
type __EnumValue {
  name: String!
  description: String
  isDeprecated: Boolean!
  deprecationReason: String
}
*/

/*
type __Directive {
  name: String!
  description: String
  locations: [__DirectiveLocation!]!
  args: [__InputValue!]!
}

enum __DirectiveLocation {
  QUERY
  MUTATION
  SUBSCRIPTION
  FIELD
  FRAGMENT_DEFINITION
  FRAGMENT_SPREAD
  INLINE_FRAGMENT
}
*/

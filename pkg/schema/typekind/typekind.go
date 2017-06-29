package typekind

type TypeKind string

const (
	Scalar      TypeKind = "SCALAR"
	Object      TypeKind = "OBJECT"
	Interface   TypeKind = "INTERFACE"
	Union       TypeKind = "UNION"
	Enum        TypeKind = "ENUM"
	InputObject TypeKind = "INPUT_OBJECT"
	List        TypeKind = "LIST"
	NonNull     TypeKind = "NON_NULL"
)

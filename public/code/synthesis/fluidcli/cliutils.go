//go:build llm_generated_gemini3pro

package fluidcli

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/golangci/gofmt/gofmt"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/unsafeperf"
	"github.com/urfave/cli/v3"
)

// FlagHandler defines the strategy for handling a specific Go type mapping to a CLI flag.
type FlagHandler interface {
	// GoType returns the native Go type as a string (e.g., "int", "string", "bool").
	GoType() string
	// Imports returns a list of packages required for the conversion code (e.g., "strconv").
	Imports() []string
	// GenerateAppend returns the Go code snippet to append this flag to the 'args' slice.
	// flagName is the CLI flag (e.g., "--count").
	// fieldName is the struct field access (e.g., "inst.Count").
	GenerateAppend(flagName, fieldName string) string
}

// ArgumentHandler defines the strategy for handling a specific Go type mapping to a CLI argument.
type ArgumentHandler interface {
	// GoType returns the native Go type as a string (e.g., "int", "string", "bool").
	GoType() string
	// Imports returns a list of packages required for the conversion code (e.g., "strconv").
	Imports() []string
	// GenerateAppend returns the Go code snippet to append this argument to the 'args' slice.
	// fieldName is the struct field access (e.g., "inst.Arg").
	GenerateAppend(fieldName string) string
}

// -----------------------------------------------------------------------------
// Concrete Handlers
// -----------------------------------------------------------------------------

type stringHandler struct{}

func (h stringHandler) GoType() string    { return "string" }
func (h stringHandler) Imports() []string { return nil }
func (h stringHandler) GenerateAppend(flag, field string) string {
	return fmt.Sprintf(`	if %s != "" {
		args = append(args, %q, %s)
	}`, field, flag, field)
}

type boolHandler struct{}

func (h boolHandler) GoType() string    { return "bool" }
func (h boolHandler) Imports() []string { return nil }
func (h boolHandler) GenerateAppend(flag, field string) string {
	return fmt.Sprintf(`	if %s {
		args = append(args, %q)
	}`, field, flag)
}

type intHandler struct{}

func (h intHandler) GoType() string    { return "int64" }
func (h intHandler) Imports() []string { return []string{"strconv"} }
func (h intHandler) GenerateAppend(flag, field string) string {
	return fmt.Sprintf(`	if %s != 0 {
		args = append(args, %q, strconv.FormatInt(%s, 10))
	}`, field, flag, field)
}

type floatHandler struct{}

func (h floatHandler) GoType() string    { return "float64" }
func (h floatHandler) Imports() []string { return []string{"fmt"} }
func (h floatHandler) GenerateAppend(flag, field string) string {
	return fmt.Sprintf(`	if %s != 0.0 {
		args = append(args, %q, fmt.Sprintf("%%f", %s))
	}`, field, flag, field)
}

type stringSliceHandler struct{}

func (h stringSliceHandler) GoType() string    { return "[]string" }
func (h stringSliceHandler) Imports() []string { return nil }
func (h stringSliceHandler) GenerateAppend(flag, field string) string {
	return fmt.Sprintf(`	if len(%s) > 0 {
		for _, v := range %s {
			args = append(args, %q, v)
		}
	}`, field, field, flag)
}

// -----------------------------------------------------------------------------
// Concrete Argument Handlers
// -----------------------------------------------------------------------------

type argStringHandler struct{}

func (h argStringHandler) GoType() string    { return "string" }
func (h argStringHandler) Imports() []string { return nil }
func (h argStringHandler) GenerateAppend(field string) string {
	return fmt.Sprintf(`	if %s != "" {
		args = append(args, %s)
	}`, field, field)
}

type argBoolHandler struct{}

func (h argBoolHandler) GoType() string    { return "bool" }
func (h argBoolHandler) Imports() []string { return nil }
func (h argBoolHandler) GenerateAppend(field string) string {
	return fmt.Sprintf(`	if %s {
		args = append(args, "true")
	}`, field)
}

type argIntHandler struct{}

func (h argIntHandler) GoType() string    { return "int64" }
func (h argIntHandler) Imports() []string { return []string{"strconv"} }
func (h argIntHandler) GenerateAppend(field string) string {
	return fmt.Sprintf(`	if %s != 0 {
		args = append(args, strconv.FormatInt(%s, 10))
	}`, field, field)
}

type argFloatHandler struct{}

func (h argFloatHandler) GoType() string    { return "float64" }
func (h argFloatHandler) Imports() []string { return []string{"fmt"} }
func (h argFloatHandler) GenerateAppend(field string) string {
	return fmt.Sprintf(`	if %s != 0.0 {
		args = append(args, fmt.Sprintf("%%f", %s))
	}`, field, field)
}

type argStringSliceHandler struct{}

func (h argStringSliceHandler) GoType() string    { return "[]string" }
func (h argStringSliceHandler) Imports() []string { return nil }
func (h argStringSliceHandler) GenerateAppend(field string) string {
	return fmt.Sprintf(`	if len(%s) > 0 {
		args = append(args, %s...)
	}`, field, field)
}

// -----------------------------------------------------------------------------
// Generator
// -----------------------------------------------------------------------------

// GenerateFluidWrapper writes the Go source code for a fluent API builder for the given command.
func GenerateFluidWrapper(toolName naming.StylableName, cmd *cli.Command, pkgName string) (code string, err error) {
	if cmd == nil {
		err = eh.Errorf("cmd is nil")
		return
	}

	// 1. Analyze Flags and identify strategies
	type flagInfo struct {
		Name      string // CLI Name (kebab-case)
		FieldName naming.StylableName
		Usage     string
		Handler   FlagHandler
	}

	var flags []flagInfo
	seenFlags := containers.NewHashSet[naming.StylableName](16)
	requiredImports := containers.NewHashSet[string](8)

	// Always need these
	requiredImports.Add("context")
	requiredImports.Add("os/exec")

	for _, f := range cmd.Flags {
		names := f.Names()
		if len(names) == 0 {
			continue
		}
		mainName := names[0]
		var fieldName naming.StylableName
		fieldName, err = naming.MakeStylableName(mainName)

		if seenFlags.AddEx(fieldName) {
			continue
		}

		info := flagInfo{
			Name:      mainName,
			FieldName: fieldName,
		}

		// Determine Handler based on cli.Flag type
		switch v := f.(type) {
		case *cli.BoolFlag:
			info.Handler = boolHandler{}
			info.Usage = v.Usage
		case *cli.StringFlag:
			info.Handler = stringHandler{}
			info.Usage = v.Usage
		case *cli.IntFlag:
			info.Handler = intHandler{}
			info.Usage = v.Usage
		case *cli.FloatFlag:
			info.Handler = floatHandler{}
			info.Usage = v.Usage
		case *cli.StringSliceFlag:
			info.Handler = stringSliceHandler{}
			info.Usage = v.Usage
		default:
			// Fallback
			info.Handler = stringHandler{}
		}

		// Collect imports
		for _, imp := range info.Handler.Imports() {
			requiredImports.Add(imp)
		}

		flags = append(flags, info)
	}

	// 1b. Analyze Arguments and identify strategies
	type argumentInfo struct {
		Name      string // CLI Name
		FieldName naming.StylableName
		Usage     string
		Required  bool
		Handler   ArgumentHandler
	}

	var arguments []argumentInfo
	seenArgs := containers.NewHashSet[naming.StylableName](16)

	for _, a := range cmd.Arguments {
		var fieldName naming.StylableName
		fieldName, err = naming.MakeStylableName(a.Name)
		if err != nil {
			continue
		}

		if seenArgs.AddEx(fieldName) {
			continue
		}

		info := argumentInfo{
			Name:      a.Name,
			FieldName: fieldName,
			Usage:     a.Usage,
			Required:  a.Required,
		}

		// Collect imports
		for _, imp := range info.Handler.Imports() {
			requiredImports.Add(imp)
		}

		// Determine Handler based on cli.Argument type
		switch v := a.(type) {
		case *cli.BoolArgument:
			info.Handler = argBoolHandler{}
		case *cli.StringArgument:
			info.Handler = argStringHandler{}
		case *cli.IntArgument:
			info.Handler = argIntHandler{}
		case *cli.FloatArgument:
			info.Handler = argFloatHandler{}
		case *cli.StringSliceArgument:
			info.Handler = argStringSliceHandler{}
		default:
			// Fallback to string
			info.Handler = argStringHandler{}
		}

		arguments = append(arguments, info)
	}

	// 2. Start Writing Code
	b := bytes.NewBuffer(make([]byte, 0, 4096))

	// Header
	_, _ = fmt.Fprintf(b, "package %s\n\n", pkgName)
	_, _ = fmt.Fprintf(b, "import (\n")

	// sorting will be handled by gofmt
	for imp := range requiredImports.IterateAll() {
		_, _ = fmt.Fprintf(b, "\t\"%s\"\n", imp)
	}
	_, _ = fmt.Fprintf(b, ")\n\n")

	typeName := toolName.Convert(naming.UpperCamelCase) + "Builder"

	// Struct Definition
	_, _ = fmt.Fprintf(b, "// %s allows constructing and executing a %s command with a fluent API.\n", typeName, cmd.Name)
	_, _ = fmt.Fprintf(b, "type %s struct {\n", typeName)
	_, _ = fmt.Fprintf(b, "\texePath string\n")
	_, _ = fmt.Fprintf(b, "\tsubCommand string\n")

	for _, info := range flags {
		_, _ = fmt.Fprintf(b, "\t%s %s\n", info.FieldName.Convert(naming.LowerCamelCase), info.Handler.GoType())
	}

	for _, info := range arguments {
		_, _ = fmt.Fprintf(b, "\targ%s %s\n", info.FieldName.Convert(naming.UpperCamelCase), info.Handler.GoType())
	}

	_, _ = fmt.Fprintf(b, "\targs []string\n")
	_, _ = fmt.Fprintf(b, "}\n\n")

	// Constructor
	_, _ = fmt.Fprintf(b, "func New%s(exePath string) *%s {\n", toolName.Convert(naming.UpperCamelCase), typeName)
	_, _ = fmt.Fprintf(b, "\tif exePath == \"\" {\n")
	_, _ = fmt.Fprintf(b, "\t\texePath = lookupDefaultExePath(%q)\n", cmd.Name)
	_, _ = fmt.Fprintf(b, "\t}\n")
	_, _ = fmt.Fprintf(b, "\treturn &%s{\n", typeName)
	_, _ = fmt.Fprintf(b, "\t\texePath: exePath,\n")
	_, _ = fmt.Fprintf(b, "\t}\n")
	_, _ = fmt.Fprintf(b, "}\n\n")

	// Cmd Method
	_, _ = fmt.Fprintf(b, "// Cmd returns the exec.Cmd struct for the constructed command.\n")
	_, _ = fmt.Fprintf(b, "func (inst *%s) Cmd(ctx context.Context) *exec.Cmd {\n", typeName)
	_, _ = fmt.Fprintf(b, "\treturn exec.CommandContext(ctx, inst.exePath, inst.BuildArgs()...)\n")
	_, _ = fmt.Fprintf(b, "}\n\n")

	// BuildArgs Method
	_, _ = fmt.Fprintf(b, "// BuildArgs constructs the list of arguments for the command.\n")
	_, _ = fmt.Fprintf(b, "func (inst *%s) BuildArgs() []string {\n", typeName)
	// Heuristic allocation
	_, _ = fmt.Fprintf(b, "\t// Estimate capacity: 1 subcommand + ~%d flags + ~%d arguments + positional args\n", len(flags), len(arguments))
	_, _ = fmt.Fprintf(b, "\targs := make([]string, 0, 1 + %d + %d + len(inst.args))\n\n", len(flags), len(arguments))

	_, _ = fmt.Fprintf(b, "\tif inst.subCommand != \"\" {\n")
	_, _ = fmt.Fprintf(b, "\t\targs = append(args, inst.subCommand)\n")
	_, _ = fmt.Fprintf(b, "\t}\n\n")

	// Generate conversion code for each flag
	for _, info := range flags {
		flagPrefix := "--"
		if len(info.Name) == 1 {
			flagPrefix = "-"
		}
		flagArg := flagPrefix + info.Name
		fieldAccess := "inst." + info.FieldName.Convert(naming.LowerCamelCase).String()

		code = info.Handler.GenerateAppend(flagArg, fieldAccess)
		_, _ = fmt.Fprintf(b, "%s\n", code)
	}

	// Generate conversion code for each argument
	for _, info := range arguments {
		fieldAccess := "inst.arg" + info.FieldName.Convert(naming.UpperCamelCase).String()

		code = info.Handler.GenerateAppend(fieldAccess)
		_, _ = fmt.Fprintf(b, "%s\n", code)
	}

	_, _ = fmt.Fprintf(b, "\n\tif len(inst.args) > 0 {\n")
	_, _ = fmt.Fprintf(b, "\t\targs = append(args, inst.args...)\n")
	_, _ = fmt.Fprintf(b, "\t}\n\n")
	_, _ = fmt.Fprintf(b, "\treturn args\n")
	_, _ = fmt.Fprintf(b, "}\n\n")

	// Positional Argument Helpers
	_, _ = fmt.Fprintf(b, "// AddArg adds a positional argument to the command.\n")
	_, _ = fmt.Fprintf(b, "func (inst *%s) AddArg(arg string) *%s {\n", typeName, typeName)
	_, _ = fmt.Fprintf(b, "\tinst.args = append(inst.args, arg)\n")
	_, _ = fmt.Fprintf(b, "\treturn inst\n")
	_, _ = fmt.Fprintf(b, "}\n\n")

	_, _ = fmt.Fprintf(b, "// AddArgs adds multiple positional arguments to the command.\n")
	_, _ = fmt.Fprintf(b, "func (inst *%s) AddArgs(args ...string) *%s {\n", typeName, typeName)
	_, _ = fmt.Fprintf(b, "\tinst.args = append(inst.args, args...)\n")
	_, _ = fmt.Fprintf(b, "\treturn inst\n")
	_, _ = fmt.Fprintf(b, "}\n\n")

	// Subcommands
	cmdNames := containers.NewHashSet[string](8)

	for _, c := range cmd.Commands {
		var methodName naming.StylableName
		methodName, err = naming.MakeStylableName(c.Name)
		if err != nil {
			err = eh.Errorf("unable to create method name: %w", err)
			return
		}
		cmdNames.Add(methodName.Convert(naming.DefaultNamingStyle).String())
		_, _ = fmt.Fprintf(b, "// %s sets the subcommand to %q.\n", methodName.Convert(naming.UpperCamelCase), c.Name)
		if c.Usage != "" {
			_, _ = fmt.Fprintf(b, "// %s\n", formatDoc(c.Usage))
		}
		_, _ = fmt.Fprintf(b, "func (inst *%s) %s() *%s {\n", typeName, methodName.Convert(naming.UpperCamelCase), typeName)
		_, _ = fmt.Fprintf(b, "\tinst.subCommand = %q\n", c.Name)
		_, _ = fmt.Fprintf(b, "\treturn inst\n")
		_, _ = fmt.Fprintf(b, "}\n\n")
	}

	// Flag Setters
	for _, info := range flags {
		var methodName naming.StylableName
		methodName, err = naming.MakeStylableName(info.Name)
		if err != nil {
			err = eh.Errorf("unable to create method name: %w", err)
			return
		}
		// Resolve naming collision between Subcommand and Flag (e.g. "Version")
		if cmdNames.Has(methodName.Convert(naming.DefaultNamingStyle).String()) {
			methodName = "With" + methodName.Convert(naming.UpperCamelCase)
		}

		_, _ = fmt.Fprintf(b, "// %s sets the %q flag.\n", methodName.Convert(naming.UpperCamelCase), info.Name)
		if info.Usage != "" {
			_, _ = fmt.Fprintf(b, "// %s\n", formatDoc(info.Usage))
		}
		goType := info.Handler.GoType()
		switch goType {
		case "bool":
			_, _ = fmt.Fprintf(b, "func (inst *%s) %s() *%s {\n", typeName, methodName.Convert(naming.UpperCamelCase), typeName)
			_, _ = fmt.Fprintf(b, "\tinst.%s = true\n", info.FieldName.Convert(naming.LowerCamelCase))
			_, _ = fmt.Fprintf(b, "\treturn inst\n")
			_, _ = fmt.Fprintf(b, "}\n\n")
		default:
			_, _ = fmt.Fprintf(b, "func (inst *%s) %s(val %s) *%s {\n", typeName, methodName.Convert(naming.UpperCamelCase), goType, typeName)
			_, _ = fmt.Fprintf(b, "\tinst.%s = val\n", info.FieldName.Convert(naming.LowerCamelCase))
			_, _ = fmt.Fprintf(b, "\treturn inst\n")
			_, _ = fmt.Fprintf(b, "}\n\n")
		}
	}

	// Argument Setters
	for _, info := range arguments {
		_, _ = fmt.Fprintf(b, "// Set%s sets the %q argument.\n", info.FieldName.Convert(naming.UpperCamelCase), info.Name)
		if info.Usage != "" {
			_, _ = fmt.Fprintf(b, "// %s\n", formatDoc(info.Usage))
		}
		if info.Required {
			_, _ = fmt.Fprintf(b, "// Required.\n")
		}
		goType := info.Handler.GoType()
		switch goType {
		case "bool":
			_, _ = fmt.Fprintf(b, "func (inst *%s) Set%s(val bool) *%s {\n", typeName, info.FieldName.Convert(naming.UpperCamelCase), typeName)
			_, _ = fmt.Fprintf(b, "\tinst.arg%s = val\n", info.FieldName.Convert(naming.UpperCamelCase))
			_, _ = fmt.Fprintf(b, "\treturn inst\n")
			_, _ = fmt.Fprintf(b, "}\n\n")
		default:
			_, _ = fmt.Fprintf(b, "func (inst *%s) Set%s(val %s) *%s {\n", typeName, info.FieldName.Convert(naming.UpperCamelCase), goType, typeName)
			_, _ = fmt.Fprintf(b, "\tinst.arg%s = val\n", info.FieldName.Convert(naming.UpperCamelCase))
			_, _ = fmt.Fprintf(b, "\treturn inst\n")
			_, _ = fmt.Fprintf(b, "}\n\n")
		}
	}

	var formattedCode []byte
	formattedCode, err = gofmt.Source("generated.go", b.Bytes(), gofmt.Options{
		NeedSimplify: false,
		RewriteRules: nil,
	})
	if err != nil {
		code = b.String()
		log.Warn().Err(err).Msg("unable to format generated source code, using unformatted code")
		err = nil
	} else {
		code = unsafeperf.UnsafeBytesToString(formattedCode)
	}

	return
}

func formatDoc(s string) string {
	// Simple formatting to handle newlines in usage string if any
	return strings.ReplaceAll(strings.TrimSpace(s), "\n", "\n// ")
}

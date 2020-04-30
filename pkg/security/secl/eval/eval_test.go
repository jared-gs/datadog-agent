package eval

import (
	"fmt"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/DataDog/datadog-agent/pkg/security/secl/ast"
	"github.com/pkg/errors"
)

type testProcess struct {
	name   string
	uid    int
	isRoot bool
}

type testOpen struct {
	filename string
	flags    int
}

type testEvent struct {
	process testProcess
	open    testOpen
}

type testModel struct {
	data testEvent
}

func (m *testModel) SetData(data interface{}) {
	m.data = data.(testEvent)
}

func (m *testModel) GetEvaluator(key string) (interface{}, []string, error) {
	switch key {

	case "process.name":

		return &StringEvaluator{
			Eval:      func(ctx *Context) string { return m.data.process.name },
			DebugEval: func(ctx *Context) string { return m.data.process.name },
			Field:     key,
		}, []string{"process"}, nil

	case "process.uid":

		return &IntEvaluator{
			Eval:      func(ctx *Context) int { return m.data.process.uid },
			DebugEval: func(ctx *Context) int { return m.data.process.uid },
			Field:     key,
		}, []string{"process"}, nil

	case "process.is_root":

		return &BoolEvaluator{
			Eval:      func(ctx *Context) bool { return m.data.process.isRoot },
			DebugEval: func(ctx *Context) bool { return m.data.process.isRoot },
			Field:     key,
		}, []string{"process"}, nil

	case "open.filename":

		return &StringEvaluator{
			Eval:      func(ctx *Context) string { return m.data.open.filename },
			DebugEval: func(ctx *Context) string { return m.data.open.filename },
			Field:     key,
		}, []string{"fs"}, nil

	case "open.flags":

		return &IntEvaluator{
			Eval:      func(ctx *Context) int { return m.data.open.flags },
			DebugEval: func(ctx *Context) int { return m.data.open.flags },
			Field:     key,
		}, []string{"fs"}, nil

	}

	return nil, nil, errors.Wrap(ErrFieldNotFound, key)
}

func parse(t *testing.T, expr string, macros map[string]*ast.Macro, model Model, debug bool) (*RuleEvaluator, *ast.Rule, error) {
	rule, err := ast.ParseRule(expr)
	if err != nil {
		t.Fatal(fmt.Sprintf("%s\n%s", err, expr))
	}

	evaluator, err := RuleToEvaluator(rule, macros, model, debug)
	if err != nil {
		return nil, rule, err
	}

	return evaluator, rule, err
}

func eval(t *testing.T, event *testEvent, expr string) (bool, *ast.Rule, error) {
	model := &testModel{data: *event}

	ctx := &Context{}

	evaluator, rule, err := parse(t, expr, nil, model, false)
	if err != nil {
		return false, rule, err
	}
	r1 := evaluator.Eval(ctx)

	evaluator, _, err = parse(t, expr, nil, model, true)
	if err != nil {
		return false, rule, err
	}
	r2 := evaluator.Eval(ctx)

	if r1 != r2 {
		t.Fatalf("different result for non-debug and debug evalutators with rule `%s`", expr)
	}

	return r1, rule, nil
}

func TestStringError(t *testing.T) {
	event := &testEvent{
		process: testProcess{
			name: "/usr/bin/cat",
			uid:  1,
		},
		open: testOpen{
			filename: "/etc/shadow",
		},
	}

	_, _, err := eval(t, event, `process.name != "/usr/bin/vipw" && process.uid != 0 && open.filename == 3`)
	if err == nil || err.(*AstToEvalError).Pos.Column != 73 {
		t.Fatal("should report a string type error")
	}
}

func TestIntError(t *testing.T) {
	event := &testEvent{
		process: testProcess{
			name: "/usr/bin/cat",
			uid:  1,
		},
		open: testOpen{
			filename: "/etc/shadow",
		},
	}

	_, _, err := eval(t, event, `process.name != "/usr/bin/vipw" && process.uid != "test" && Open.Filename == "/etc/shadow"`)
	if err == nil || err.(*AstToEvalError).Pos.Column != 51 {
		t.Fatal("should report a string type error")
	}
}

func TestBoolError(t *testing.T) {
	event := &testEvent{
		process: testProcess{
			name: "/usr/bin/cat",
			uid:  1,
		},
		open: testOpen{
			filename: "/etc/shadow",
		},
	}

	_, _, err := eval(t, event, `(process.name != "/usr/bin/vipw") == "test"`)
	if err == nil || err.(*AstToEvalError).Pos.Column != 38 {
		t.Fatal("should report a bool type error")
	}
}

func TestSimpleString(t *testing.T) {
	event := &testEvent{
		process: testProcess{
			name: "/usr/bin/cat",
			uid:  1,
		},
	}

	tests := []struct {
		Expr     string
		Expected bool
	}{
		{Expr: `process.name != "/usr/bin/vipw"`, Expected: true},
		{Expr: `process.name != "/usr/bin/cat"`, Expected: false},
		{Expr: `process.name == "/usr/bin/cat"`, Expected: true},
		{Expr: `process.name == "/usr/bin/vipw"`, Expected: false},
		{Expr: `(process.name == "/usr/bin/cat" && process.uid == 0) && (process.name == "/usr/bin/cat" && process.uid == 0)`, Expected: false},
		{Expr: `(process.name == "/usr/bin/cat" && process.uid == 1) && (process.name == "/usr/bin/cat" && process.uid == 1)`, Expected: true},
	}

	for _, test := range tests {
		result, _, err := eval(t, event, test.Expr)
		if err != nil {
			t.Fatalf("error while evaluating `%s`: %s", test.Expr, err)
		}

		if result != test.Expected {
			t.Errorf("expected result `%t` not found, got `%t`\n%s", test.Expected, result, test.Expr)
		}
	}
}

func TestSimpleInt(t *testing.T) {
	event := &testEvent{
		process: testProcess{
			uid: 444,
		},
	}

	tests := []struct {
		Expr     string
		Expected bool
	}{
		{Expr: `111 != 555`, Expected: true},
		{Expr: `process.uid != 555`, Expected: true},
		{Expr: `process.uid != 444`, Expected: false},
		{Expr: `process.uid == 444`, Expected: true},
		{Expr: `process.uid == 555`, Expected: false},
		{Expr: `--3 == 3`, Expected: true},
		{Expr: `3 ^ 3 == 0`, Expected: true},
		{Expr: `^0 == -1`, Expected: true},
	}

	for _, test := range tests {
		result, _, err := eval(t, event, test.Expr)
		if err != nil {
			t.Fatalf("error while evaluating `%s`: %s", test.Expr, err)
		}

		if result != test.Expected {
			t.Errorf("expected result `%t` not found, got `%t`\n%s", test.Expected, result, test.Expr)
		}
	}
}

func TestSimpleBool(t *testing.T) {
	event := &testEvent{}

	tests := []struct {
		Expr     string
		Expected bool
	}{
		{Expr: `(444 == 444) && ("test" == "test")`, Expected: true},
		{Expr: `(444 != 444) && ("test" == "test")`, Expected: false},
		{Expr: `(444 != 555) && ("test" == "test")`, Expected: true},
		{Expr: `(444 != 555) && ("test" != "aaaa")`, Expected: true},
	}

	for _, test := range tests {
		result, _, err := eval(t, event, test.Expr)
		if err != nil {
			t.Fatalf("error while evaluating `%s`: %s", test.Expr, err)
		}

		if result != test.Expected {
			t.Errorf("expected result `%t` not found, got `%t`\n%s", test.Expected, result, test.Expr)
		}
	}
}

func TestSyscallConst(t *testing.T) {
	event := &testEvent{}

	tests := []struct {
		Expr     string
		Expected bool
	}{
		{Expr: fmt.Sprintf(`%d == S_IEXEC`, syscall.S_IEXEC), Expected: true},
	}

	for _, test := range tests {
		result, _, err := eval(t, event, test.Expr)
		if err != nil {
			t.Fatalf("error while evaluating `%s`: %s", test.Expr, err)
		}

		if result != test.Expected {
			t.Errorf("expected result `%t` not found, got `%t`\n%s", test.Expected, result, test.Expr)
		}
	}
}

func TestPrecedence(t *testing.T) {
	event := &testEvent{}

	tests := []struct {
		Expr     string
		Expected bool
	}{
		{Expr: `false || (true != true)`, Expected: false},
		{Expr: `false || true`, Expected: true},
		{Expr: `1 == 1 & 1`, Expected: true},
	}

	for _, test := range tests {
		result, _, err := eval(t, event, test.Expr)
		if err != nil {
			t.Fatalf("error while evaluating `%s`: %s", test.Expr, err)
		}

		if result != test.Expected {
			t.Errorf("expected result `%t` not found, got `%t`\n%s", test.Expected, result, test.Expr)
		}
	}
}

func TestParenthesis(t *testing.T) {
	event := &testEvent{}

	tests := []struct {
		Expr     string
		Expected bool
	}{
		{Expr: `(true) == (true)`, Expected: true},
	}

	for _, test := range tests {
		result, _, err := eval(t, event, test.Expr)
		if err != nil {
			t.Fatalf("error while evaluating `%s`: %s", test.Expr, err)
		}

		if result != test.Expected {
			t.Errorf("expected result `%t` not found, got `%t`\n%s", test.Expected, result, test.Expr)
		}
	}
}

func TestSimpleBitOperations(t *testing.T) {
	event := &testEvent{}

	tests := []struct {
		Expr     string
		Expected bool
	}{
		{Expr: `(3 & 3) == 3`, Expected: true},
		{Expr: `(3 & 1) == 3`, Expected: false},
		{Expr: `(2 | 1) == 3`, Expected: true},
		{Expr: `(3 & 1) != 0`, Expected: true},
		{Expr: `0 != 3 & 1`, Expected: true},
		{Expr: `(3 ^ 3) == 0`, Expected: true},
	}

	for _, test := range tests {
		result, _, err := eval(t, event, test.Expr)
		if err != nil {
			t.Fatalf("error while evaluating `%s`", test.Expr)
		}

		if result != test.Expected {
			t.Errorf("expected result `%t` not found, got `%t`\n%s", test.Expected, result, test.Expr)
		}
	}
}

func TestRegexp(t *testing.T) {
	event := &testEvent{
		process: testProcess{
			name: "/usr/bin/cat",
		},
	}

	tests := []struct {
		Expr     string
		Expected bool
	}{
		{Expr: `process.name =~ "/usr/bin/*"`, Expected: true},
		{Expr: `process.name =~ "/usr/sbin/*"`, Expected: false},
		{Expr: `process.name !~ "/usr/sbin/*"`, Expected: true},
	}

	for _, test := range tests {
		result, _, err := eval(t, event, test.Expr)
		if err != nil {
			t.Fatalf("error while evaluating `%s`: %s", test.Expr, err)
		}

		if result != test.Expected {
			t.Errorf("expected result `%t` not found, got `%t`\n%s", test.Expected, result, test.Expr)
		}
	}
}

func TestInArray(t *testing.T) {
	event := &testEvent{
		process: testProcess{
			name: "a",
			uid:  3,
		},
	}

	tests := []struct {
		Expr     string
		Expected bool
	}{
		{Expr: `"a" in [ "a", "b", "c" ]`, Expected: true},
		{Expr: `process.name in [ "c", "b", "a" ]`, Expected: true},
		{Expr: `"d" in [ "a", "b", "c" ]`, Expected: false},
		{Expr: `process.name in [ "c", "b", "z" ]`, Expected: false},
		{Expr: `"a" not in [ "a", "b", "c" ]`, Expected: false},
		{Expr: `process.name not in [ "c", "b", "a" ]`, Expected: false},
		{Expr: `"d" not in [ "a", "b", "c" ]`, Expected: true},
		{Expr: `process.name not in [ "c", "b", "z" ]`, Expected: true},
		{Expr: `3 in [ 1, 2, 3 ]`, Expected: true},
		{Expr: `process.uid in [ 1, 2, 3 ]`, Expected: true},
		{Expr: `4 in [ 1, 2, 3 ]`, Expected: false},
		{Expr: `process.uid in [ 4, 2, 1 ]`, Expected: false},
		{Expr: `3 not in [ 1, 2, 3 ]`, Expected: false},
		{Expr: `3 not in [ 1, 2, 3 ]`, Expected: false},
		{Expr: `4 not in [ 1, 2, 3 ]`, Expected: true},
		{Expr: `4 not in [ 3, 2, 1 ]`, Expected: true},
	}

	for _, test := range tests {
		result, _, err := eval(t, event, test.Expr)
		if err != nil {
			t.Fatalf("error while evaluating `%s: %s`", test.Expr, err)
		}

		if result != test.Expected {
			t.Errorf("expected result `%t` not found, got `%t`\n%s", test.Expected, result, test.Expr)
		}
	}
}

func TestComplex(t *testing.T) {
	event := &testEvent{
		open: testOpen{
			filename: "/var/lib/httpd/htpasswd",
			flags:    syscall.O_CREAT | syscall.O_TRUNC | syscall.O_EXCL | syscall.O_RDWR | syscall.O_WRONLY,
		},
	}

	tests := []struct {
		Expr     string
		Expected bool
	}{
		{Expr: `open.filename =~ "/var/lib/httpd/*" && open.flags & (O_CREAT | O_TRUNC | O_EXCL | O_RDWR | O_WRONLY) > 0`, Expected: true},
	}

	for _, test := range tests {
		result, _, err := eval(t, event, test.Expr)
		if err != nil {
			t.Fatalf("error while evaluating `%s: %s`", test.Expr, err)
		}

		if result != test.Expected {
			t.Errorf("expected result `%t` not found, got `%t`\n%s", test.Expected, result, test.Expr)
		}
	}
}

func TestTags(t *testing.T) {
	expr := `process.name != "/usr/bin/vipw" && open.filename == "/etc/passwd"`
	evaluator, _, err := parse(t, expr, nil, &testModel{}, false)
	if err != nil {
		t.Fatal(fmt.Sprintf("%s\n%s", err, expr))
	}

	expected := []string{"fs", "process"}

	if !reflect.DeepEqual(evaluator.Tags, expected) {
		t.Errorf("tags expected not %+v != %+v", expected, evaluator.Tags)
	}
}

func TestPartial(t *testing.T) {
	event := testEvent{
		process: testProcess{
			name:   "abc",
			uid:    123,
			isRoot: true,
		},
		open: testOpen{
			filename: "xyz",
		},
	}

	tests := []struct {
		Expr          string
		Field         string
		IsDiscrimator bool
	}{
		{Expr: `true || process.name == "/usr/bin/cat"`, Field: "process.name", IsDiscrimator: false},
		{Expr: `false || process.name == "/usr/bin/cat"`, Field: "process.name", IsDiscrimator: true},
		{Expr: `true || process.name == "abc"`, Field: "process.name", IsDiscrimator: false},
		{Expr: `false || process.name == "abc"`, Field: "process.name", IsDiscrimator: false},
		{Expr: `true && process.name == "/usr/bin/cat"`, Field: "process.name", IsDiscrimator: true},
		{Expr: `false && process.name == "/usr/bin/cat"`, Field: "process.name", IsDiscrimator: true},
		{Expr: `true && process.name == "abc"`, Field: "process.name", IsDiscrimator: false},
		{Expr: `false && process.name == "abc"`, Field: "process.name", IsDiscrimator: true},
		{Expr: `open.filename == "test1" && process.name == "/usr/bin/cat"`, Field: "process.name", IsDiscrimator: true},
		{Expr: `open.filename == "test1" && process.name != "/usr/bin/cat"`, Field: "process.name", IsDiscrimator: false},
		{Expr: `open.filename == "test1" || process.name == "/usr/bin/cat"`, Field: "process.name", IsDiscrimator: false},
		{Expr: `open.filename == "test1" || process.name != "/usr/bin/cat"`, Field: "process.name", IsDiscrimator: false},
		{Expr: `open.filename == "test1" && !(process.name == "/usr/bin/cat")`, Field: "process.name", IsDiscrimator: false},
		{Expr: `open.filename == "test1" && !(process.name != "/usr/bin/cat")`, Field: "process.name", IsDiscrimator: true},
		{Expr: `open.filename == "test1" && (process.name =~ "/usr/bin/*" )`, Field: "process.name", IsDiscrimator: true},
		{Expr: `open.filename == "test1" && process.name =~ "ab*" `, Field: "process.name", IsDiscrimator: false},
		{Expr: `open.filename == "test1" && process.name == open.filename`, Field: "process.name", IsDiscrimator: false},
		{Expr: `open.filename =~ "test1" && process.name == "abc"`, Field: "process.name", IsDiscrimator: false},
		{Expr: `open.filename in [ "test1", "test2" ] && (process.name == open.filename)`, Field: "process.name", IsDiscrimator: false},
		{Expr: `open.filename in [ "test1", "test2" ] && process.name == "abc"`, Field: "process.name", IsDiscrimator: false},
		{Expr: `!(open.filename in [ "test1", "test2" ]) && process.name == "abc"`, Field: "process.name", IsDiscrimator: false},
		{Expr: `!(open.filename in [ "test1", "xyz" ]) && process.name == "abc"`, Field: "process.name", IsDiscrimator: false},
		{Expr: `!(open.filename in [ "test1", "xyz" ]) && process.name == "abc"`, Field: "process.name", IsDiscrimator: false},
		{Expr: `!(open.filename in [ "test1", "xyz" ] && true) && process.name == "abc"`, Field: "process.name", IsDiscrimator: false},
		{Expr: `!(open.filename in [ "test1", "xyz" ] && false) && process.name == "abc"`, Field: "process.name", IsDiscrimator: false},
		{Expr: `!(open.filename in [ "test1", "xyz" ] && false) && !(process.name == "abc")`, Field: "process.name", IsDiscrimator: true},
		{Expr: `!(open.filename in [ "test1", "xyz" ] && false) && !(process.name == "abc")`, Field: "open.filename", IsDiscrimator: false},
		{Expr: `(open.filename not in [ "test1", "xyz" ] && true) && !(process.name == "abc")`, Field: "open.filename", IsDiscrimator: true},
		{Expr: `open.filename == open.filename`, Field: "open.filename", IsDiscrimator: false},
		{Expr: `open.filename != open.filename`, Field: "open.filename", IsDiscrimator: true},
		{Expr: `open.filename == "test1" && process.uid == 456`, Field: "process.uid", IsDiscrimator: true},
		{Expr: `open.filename == "test1" && process.uid == 123`, Field: "process.uid", IsDiscrimator: false},
		{Expr: `open.filename == "test1" && !process.is_root`, Field: "process.is_root", IsDiscrimator: true},
		{Expr: `open.filename == "test1" && process.is_root`, Field: "process.is_root", IsDiscrimator: false},
	}

	for _, test := range tests {
		evaluator, _, err := parse(t, test.Expr, nil, &testModel{data: event}, false)
		if err != nil {
			t.Fatalf("error while evaluating `%s`: %s", test.Expr, err)
		}

		result, err := evaluator.IsDiscrimator(&Context{}, test.Field)
		if err != nil {
			t.Fatalf("error while partial evaluating `%s` for `%s`: %s", test.Expr, test.Field, err)
		}

		if result != test.IsDiscrimator {
			t.Fatalf("expected result `%t` for `%s`, got `%t`\n%s", test.IsDiscrimator, test.Field, result, test.Expr)
		}
	}
}

func TestMacroList(t *testing.T) {
	expr := `[ "/etc/shadow", "/etc/password" ]`

	macro, err := ast.ParseMacro(expr)
	if err != nil {
		t.Fatalf("%s\n%s", err, expr)
	}

	macros := map[string]*ast.Macro{
		"list": macro,
	}

	expr = `"/etc/shadow" in list`
	evaluator, _, err := parse(t, expr, macros, &testModel{data: testEvent{}}, false)
	if err != nil {
		t.Fatalf("error while evaluating `%s`: %s", expr, err)
	}

	if !evaluator.Eval(&Context{}) {
		t.Fatalf("should return true")
	}
}

func TestMacroExpression(t *testing.T) {
	expr := `open.filename in [ "/etc/shadow", "/etc/passwd" ]`

	macro, err := ast.ParseMacro(expr)
	if err != nil {
		t.Fatalf("%s\n%s", err, expr)
	}

	macros := map[string]*ast.Macro{
		"is_passwd": macro,
	}

	event := testEvent{
		process: testProcess{
			name: "httpd",
		},
		open: testOpen{
			filename: "/etc/passwd",
		},
	}

	expr = `process.name == "httpd" && is_passwd`
	evaluator, _, err := parse(t, expr, macros, &testModel{data: event}, false)
	if err != nil {
		t.Fatalf("error while evaluating `%s`: %s", expr, err)
	}

	if !evaluator.Eval(&Context{}) {
		t.Fatalf("should return true")
	}
}

func TestMacroPartial(t *testing.T) {
	expr := `open.filename in [ "/etc/shadow", "/etc/passwd" ]`

	macro, err := ast.ParseMacro(expr)
	if err != nil {
		t.Fatalf("%s\n%s", err, expr)
	}

	macros := map[string]*ast.Macro{
		"is_passwd": macro,
	}

	event := testEvent{
		open: testOpen{
			filename: "/etc/hosts",
		},
	}

	expr = `is_passwd`
	evaluator, _, err := parse(t, expr, macros, &testModel{data: event}, false)
	if err != nil {
		t.Fatalf("error while evaluating `%s`: %s", expr, err)
	}

	result, err := evaluator.IsDiscrimator(&Context{}, "open.filename")
	if err != nil {
		t.Fatal(err)
	}

	if !result {
		t.Fatal("should be a discriminator")
	}
}

func BenchmarkComplex(b *testing.B) {
	event := testEvent{
		process: testProcess{
			name: "/usr/bin/ls",
			uid:  1,
		},
	}

	ctx := &Context{}

	base := `(process.name == "/usr/bin/ls" && process.uid == 1)`
	var exprs []string

	for i := 0; i != 100; i++ {
		exprs = append(exprs, base)
	}

	expr := strings.Join(exprs, " && ")

	rule, err := ast.ParseRule(expr)
	if err != nil {
		b.Fatal(fmt.Sprintf("%s\n%s", err, expr))
	}

	evaluator, err := RuleToEvaluator(rule, nil, &testModel{data: event}, false)
	if err != nil {
		b.Fatal(fmt.Sprintf("%s\n%s", err, expr))
	}

	for i := 0; i < b.N; i++ {
		if evaluator.Eval(ctx) != true {
			b.Fatal("unexpected result")
		}
	}
}

func BenchmarkPartial(b *testing.B) {
	event := testEvent{
		process: testProcess{
			name: "abc",
			uid:  1,
		},
	}

	ctx := &Context{}

	base := `(process.name == "/usr/bin/ls" && process.uid != 0)`
	var exprs []string

	for i := 0; i != 100; i++ {
		exprs = append(exprs, base)
	}

	expr := strings.Join(exprs, " && ")

	rule, err := ast.ParseRule(expr)
	if err != nil {
		b.Fatal(fmt.Sprintf("%s\n%s", err, expr))
	}

	evaluator, err := RuleToEvaluator(rule, nil, &testModel{data: event}, false)
	if err != nil {
		b.Fatal(fmt.Sprintf("%s\n%s", err, expr))
	}

	for i := 0; i < b.N; i++ {
		if ok, _ := evaluator.IsDiscrimator(ctx, "process.name"); ok {
			b.Fatal("unexpected result")
		}
	}
}

package parser

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/crikke/ci/pkg/manifest/types"
)

// rawInput holds an unresolved task input during parse pass 1.
type rawInput struct {
	taskName   string
	outputName string
	dest       string
}

type parseState struct {
	tokens []Token
	pos    int
	src    string // used in error messages
}

// Parse converts .bongo source into a *types.Manifest.
// dir is the absolute path to the module directory.
func Parse(src, dir string) (*types.Manifest, error) {
	tokens, err := Tokenize(src)
	if err != nil {
		return nil, err
	}
	ps := &parseState{tokens: tokens, src: "build.bongo"}
	return ps.parseFile(dir)
}

func (p *parseState) peek() Token {
	if p.pos < len(p.tokens) {
		return p.tokens[p.pos]
	}
	return Token{Type: EOF}
}

func (p *parseState) consume() Token {
	t := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return t
}

func (p *parseState) expect(typ TokenType) (Token, error) {
	t := p.consume()
	if t.Type != typ {
		return t, p.errorf(t, "expected %s, got %q", typ, t.Value)
	}
	return t, nil
}

func (p *parseState) expectIdent(val string) (Token, error) {
	t := p.consume()
	if t.Type != IDENT || t.Value != val {
		return t, p.errorf(t, "expected %q, got %q", val, t.Value)
	}
	return t, nil
}

func (p *parseState) errorf(t Token, format string, args ...interface{}) error {
	return fmt.Errorf("%s:%d:%d: %s", p.src, t.Line, t.Col, fmt.Sprintf(format, args...))
}

func (p *parseState) parseFile(dir string) (*types.Manifest, error) {
	// BONGOVER = INT NEWLINE
	if _, err := p.expectIdent("BONGOVER"); err != nil {
		return nil, err
	}
	if _, err := p.expect(EQUALS); err != nil {
		return nil, err
	}
	verTok, err := p.expect(INT)
	if err != nil {
		return nil, err
	}
	version, _ := strconv.Atoi(verTok.Value)
	if _, err := p.expect(NEWLINE); err != nil {
		return nil, err
	}

	mod, err := p.parseModule(dir)
	if err != nil {
		return nil, err
	}

	taskMap := make(map[string]*types.Task)
	rawInputsMap := make(map[string][]rawInput)

	for p.peek().Type == IDENT {
		task, raws, err := p.parseTask()
		if err != nil {
			return nil, err
		}
		if _, exists := taskMap[task.Name]; exists {
			return nil, fmt.Errorf("duplicate task name %q", task.Name)
		}
		taskMap[task.Name] = task
		rawInputsMap[task.Name] = raws
	}

	if tok := p.peek(); tok.Type != EOF {
		return nil, p.errorf(tok, "unexpected token %q", tok.Value)
	}

	// Pass 2: resolve Input.Task pointers
	for taskName, raws := range rawInputsMap {
		for _, ri := range raws {
			dep, ok := taskMap[ri.taskName]
			if !ok {
				return nil, fmt.Errorf("task %q: unknown input task %q", taskName, ri.taskName)
			}
			taskMap[taskName].Inputs = append(taskMap[taskName].Inputs, types.Input{
				Task:       dep,
				OutputName: ri.outputName,
				Dest:       ri.dest,
			})
		}
	}

	// Validate exports
	for _, exp := range mod.Exports {
		task, ok := taskMap[exp.TaskName]
		if !ok {
			return nil, fmt.Errorf("export: unknown task %q", exp.TaskName)
		}
		found := false
		for _, out := range task.Outputs {
			if out.Name == exp.OutputName {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("export: task %q has no output named %q", exp.TaskName, exp.OutputName)
		}
	}

	if err := checkCycles(taskMap); err != nil {
		return nil, err
	}

	return &types.Manifest{
		AbsPath: dir,
		Version: version,
		Module:  *mod,
		Tasks:   taskMap,
	}, nil
}

func (p *parseState) parseModule(dir string) (*types.Module, error) {
	if _, err := p.expectIdent("MODULE"); err != nil {
		return nil, err
	}
	if _, err := p.expect(COLON); err != nil {
		return nil, err
	}
	if _, err := p.expect(NEWLINE); err != nil {
		return nil, err
	}
	if _, err := p.expect(INDENT); err != nil {
		return nil, err
	}

	mod := &types.Module{}
	for p.peek().Type == IDENT {
		switch p.peek().Value {
		case "NAME":
			p.consume()
			if _, err := p.expect(EQUALS); err != nil {
				return nil, err
			}
			tok, err := p.expect(STRING)
			if err != nil {
				return nil, err
			}
			mod.Name = tok.Value
			if _, err := p.expect(NEWLINE); err != nil {
				return nil, err
			}
		case "BASE_IMAGE":
			p.consume()
			if _, err := p.expect(EQUALS); err != nil {
				return nil, err
			}
			tok, err := p.expect(STRING)
			if err != nil {
				return nil, err
			}
			mod.BaseImage = tok.Value
			if _, err := p.expect(NEWLINE); err != nil {
				return nil, err
			}
		case "INCLUDE":
			p.consume()
			if _, err := p.expect(NEWLINE); err != nil {
				return nil, err
			}
			if _, err := p.expect(INDENT); err != nil {
				return nil, err
			}
			for p.peek().Type == STRING {
				tok := p.consume()
				path := tok.Value
				if !filepath.IsAbs(path) {
					path = filepath.Clean(filepath.Join(dir, path))
				}
				mod.Include = append(mod.Include, path)
				if _, err := p.expect(NEWLINE); err != nil {
					return nil, err
				}
			}
			if _, err := p.expect(DEDENT); err != nil {
				return nil, err
			}
		case "ENV":
			p.consume()
			key, val, err := p.parseEnvLine()
			if err != nil {
				return nil, err
			}
			if _, dup := mod.Env[key]; dup {
				return nil, fmt.Errorf("module: duplicate ENV key %q", key)
			}
			if mod.Env == nil {
				mod.Env = make(map[string]string)
			}
			mod.Env[key] = val
		case "EXPORT":
			p.consume()
			if _, err := p.expect(COLON); err != nil {
				return nil, err
			}
			if _, err := p.expect(NEWLINE); err != nil {
				return nil, err
			}
			if _, err := p.expect(INDENT); err != nil {
				return nil, err
			}
			for p.peek().Type == IDENT && p.peek().Value == "INPUT" {
				p.consume()
				taskTok, err := p.expect(IDENT)
				if err != nil {
					return nil, err
				}
				outTok, err := p.expect(IDENT)
				if err != nil {
					return nil, err
				}
				mod.Exports = append(mod.Exports, types.Export{
					TaskName:   taskTok.Value,
					OutputName: outTok.Value,
				})
				if _, err := p.expect(NEWLINE); err != nil {
					return nil, err
				}
			}
			if _, err := p.expect(DEDENT); err != nil {
				return nil, err
			}
		default:
			tok := p.peek()
			return nil, p.errorf(tok, "unexpected module statement %q", tok.Value)
		}
	}

	if _, err := p.expect(DEDENT); err != nil {
		return nil, err
	}

	if mod.Name == "" {
		return nil, fmt.Errorf("manifest is missing MODULE.NAME")
	}
	if mod.BaseImage == "" {
		return nil, fmt.Errorf("manifest is missing MODULE.BASE_IMAGE")
	}
	return mod, nil
}

func (p *parseState) parseTask() (*types.Task, []rawInput, error) {
	nameTok, err := p.expect(IDENT)
	if err != nil {
		return nil, nil, err
	}
	if _, err := p.expect(COLON); err != nil {
		return nil, nil, err
	}
	if _, err := p.expect(NEWLINE); err != nil {
		return nil, nil, err
	}
	if _, err := p.expect(INDENT); err != nil {
		return nil, nil, err
	}

	task := &types.Task{Name: nameTok.Value, Cache: true}
	var raws []rawInput

	for p.peek().Type == IDENT {
		switch p.peek().Value {
		case "CACHE":
			p.consume()
			tok, err := p.expect(IDENT)
			if err != nil {
				return nil, nil, err
			}
			switch strings.ToUpper(tok.Value) {
			case "TRUE":
				task.Cache = true
			case "FALSE":
				task.Cache = false
			default:
				return nil, nil, p.errorf(tok, "CACHE must be TRUE or FALSE, got %q", tok.Value)
			}
			if _, err := p.expect(NEWLINE); err != nil {
				return nil, nil, err
			}
		case "CMD":
			p.consume()
			tok, err := p.expect(STRING)
			if err != nil {
				return nil, nil, err
			}
			task.Cmd = toPtr(tok.Value)
			if _, err := p.expect(NEWLINE); err != nil {
				return nil, nil, err
			}
		case "DOCKERFILE":
			p.consume()
			pathTok, err := p.expect(STRING)
			if err != nil {
				return nil, nil, err
			}
			task.Dockerfile = toPtr(pathTok.Value)
			outTok, err := p.expect(STRING)
			if err != nil {
				return nil, nil, err
			}
			task.DockerfileOutput = toPtr(outTok.Value)
			if _, err := p.expect(NEWLINE); err != nil {
				return nil, nil, err
			}
		case "OUTPUT":
			p.consume()
			outName, err := p.expect(STRING)
			if err != nil {
				return nil, nil, err
			}
			outPath, err := p.expect(STRING)
			if err != nil {
				return nil, nil, err
			}
			task.Outputs = append(task.Outputs, types.Output{Name: outName.Value, Path: outPath.Value})
			if _, err := p.expect(NEWLINE); err != nil {
				return nil, nil, err
			}
		case "INPUT":
			p.consume()
			taskTok, err := p.expect(IDENT)
			if err != nil {
				return nil, nil, err
			}
			outTok, err := p.expect(IDENT)
			if err != nil {
				return nil, nil, err
			}
			var dest string
			if p.peek().Type == STRING {
				dest = p.consume().Value
			}
			raws = append(raws, rawInput{taskName: taskTok.Value, outputName: outTok.Value, dest: dest})
			if _, err := p.expect(NEWLINE); err != nil {
				return nil, nil, err
			}
		case "ENV":
			p.consume()
			key, val, err := p.parseEnvLine()
			if err != nil {
				return nil, nil, err
			}
			if _, dup := task.Env[key]; dup {
				return nil, nil, fmt.Errorf("task %q: duplicate ENV key %q", task.Name, key)
			}
			if task.Env == nil {
				task.Env = make(map[string]string)
			}
			task.Env[key] = val
		default:
			tok := p.peek()
			return nil, nil, p.errorf(tok, "unexpected task statement %q", tok.Value)
		}
	}

	if _, err := p.expect(DEDENT); err != nil {
		return nil, nil, err
	}
	return task, raws, nil
}

func toPtr(str string) *string {
	return &str
}

// parseEnvLine consumes `KEY "value" NEWLINE` after the caller has already consumed `ENV`.
func (p *parseState) parseEnvLine() (string, string, error) {
	keyTok, err := p.expect(IDENT)
	if err != nil {
		return "", "", err
	}
	valTok, err := p.expect(STRING)
	if err != nil {
		return "", "", err
	}
	if _, err := p.expect(NEWLINE); err != nil {
		return "", "", err
	}
	return keyTok.Value, valTok.Value, nil
}
func checkCycles(tasks map[string]*types.Task) error {
	visited := make(map[string]bool)
	inStack := make(map[string]bool)

	var dfs func(name string) error
	dfs = func(name string) error {
		if inStack[name] {
			return fmt.Errorf("cycle detected at task %q", name)
		}
		if visited[name] {
			return nil
		}
		inStack[name] = true
		for _, inp := range tasks[name].Inputs {
			if err := dfs(inp.Task.Name); err != nil {
				return err
			}
		}
		inStack[name] = false
		visited[name] = true
		return nil
	}

	for name := range tasks {
		if err := dfs(name); err != nil {
			return err
		}
	}
	return nil
}

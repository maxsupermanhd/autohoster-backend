package main

import "fmt"

func instanceChatCommandHandlerSet(inst *instance, args string, e chatCommandExecutor) {
	if !isHashInstanceAdmin(inst, e.hash) {
		instWriteFmt(inst, `chat bcast ⚠ You must be an instance admin to use /set command`)
		return
	}
	type chatCommandArgument struct {
		name string
		t    string
	}
	type chatCommand struct {
		name        string
		description string
		args        []chatCommandArgument
		exec        func(args []any) string
	}
	chatCommands := []chatCommand{{
		name:        "allowNonLinkedJoin",
		description: "Allow/forbid non linked clients to join the lobby",
		args: []chatCommandArgument{{
			name: "allow",
			t:    "bool",
		}},
		exec: func(args []any) string {
			v := args[0].(bool)
			inst.cfgs[0].Set(v, "allowNonLinkedJoin")
			return fmt.Sprintf("☑ Top configuration layer value 'allowNonLinkedJoin' was set to %+#v", v)
		},
	}, {
		name:        "allowNonLinkedPlay",
		description: "Allow/forbid non linked clients to participate in game",
		args: []chatCommandArgument{{
			name: "allow",
			t:    "bool",
		}},
		exec: func(args []any) string {
			v := args[0].(bool)
			inst.cfgs[0].Set(v, "allowNonLinkedPlay")
			return fmt.Sprintf("☑ Top configuration layer value 'allowNonLinkedPlay' was set to %+#v", v)
		},
	}, {
		name:        "allowNonLinkedChat",
		description: "Allow/forbid non linked clients to use free chat",
		args: []chatCommandArgument{{
			name: "allow",
			t:    "bool",
		}},
		exec: func(args []any) string {
			v := args[0].(bool)
			inst.cfgs[0].Set(v, "allowNonLinkedChat")
			return fmt.Sprintf("☑ Top configuration layer value 'allowNonLinkedChat' was set to %+#v", v)
		},
	}}
	type argumentParser func(arg string) (any, error)
	argumentParsers := map[string]argumentParser{
		"bool": func(arg string) (any, error) {
			if arg == "true" || arg == "t" || arg == "1" {
				return true, nil
			} else if arg == "false" || arg == "f" || arg == "0" {
				return false, nil
			} else {
				return nil, fmt.Errorf("expected 'true', 't', '1', 'false', 'f', '0' but found %q", arg)
			}
		},
	}
	showHelp := func() {
		instWriteFmt(inst, `chat direct %s %s`, e.publicKeyB64, "Allowed set parameters are:")
		for _, v := range chatCommands {
			instWriteFmt(inst, `chat direct %s %s`, e.publicKeyB64, v.name+" "+v.description)
			for i, a := range v.args {
				instWriteFmt(inst, `chat direct %s %s`, e.publicKeyB64, fmt.Sprintf("-    argument %d %s of type %s", i+1, a.name, a.t))
			}
		}
	}
	if args == "" {
		showHelp()
		return
	}

	cmd, args := popWord(args)

	for _, c := range chatCommands {
		if c.name != cmd {
			continue
		}
		var argCont string
		parsedArgs := []any{}
		for i, a := range c.args {
			argCont, args = popWord(args)
			if argCont == "" {
				instWriteFmt(inst, `chat bcast ⚠ Expected %d arguments but found %d. Use "/set" to get help.`, len(c.args), i)
				return
			}
			parser, ok := argumentParsers[a.t]
			if !ok {
				instWriteFmt(inst, `chat bcast somebody forgor to implement argument parser of type %s lmao`, a.t)
				return
			}
			val, err := parser(argCont)
			if err != nil {
				instWriteFmt(inst, `chat bcast Error parsing argument %d (%s): %s`, i+1, a.name, err.Error())
				return
			}
			parsedArgs = append(parsedArgs, val)
		}
		ret := c.exec(parsedArgs)
		instWriteFmt(inst, `chat bcast %s`, ret)
		return
	}

	instWriteFmt(inst, `chat bcast ⚠ Set parameter %q not found. Use "/set" to get help.`, cmd)
}

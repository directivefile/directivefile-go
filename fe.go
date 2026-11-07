package directivefile

// TODO
// func Adapt(path string, opts feconfig.Options) (feconfig.MachineConfig, error) {
// 	ctx := NewContext(context.Background())
// 	mc := feconfig.MachineConfig{
// 		Options: opts,
// 	}
// 	_ = mc

// 	// 4
// 	cmds := ctx.registerDirectInstantiateModule()

// 	// 5
// 	DfCmds, err := parse(path)
// 	if err != nil {
// 		return feconfig.MachineConfig{}, err
// 	}

// 	cmds = append(cmds, DfCmds.Commands...)

// 	// 6
// 	err = ctx.registerTags(cmds)
// 	if err != nil {
// 		return feconfig.MachineConfig{}, err
// 	}
// }

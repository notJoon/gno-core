package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/multierr"
	"golang.org/x/mod/module"

	"github.com/gnolang/gno/gnovm/cmd/gno/internal/pkgdownload"
	"github.com/gnolang/gno/gnovm/cmd/gno/internal/pkgdownload/rpcpkgfetcher"
	gno "github.com/gnolang/gno/gnovm/pkg/gnolang"
	"github.com/gnolang/gno/gnovm/pkg/gnomod"
	"github.com/gnolang/gno/gnovm/pkg/packages"
	"github.com/gnolang/gno/tm2/pkg/commands"
	"github.com/gnolang/gno/tm2/pkg/errors"
)

// testPackageFetcher allows to override the package fetcher during tests.
var testPackageFetcher pkgdownload.PackageFetcher

func newModCmd(io commands.IO) *commands.Command {
	cmd := commands.NewCommand(
		commands.Metadata{
			Name:       "mod",
			ShortUsage: "mod <command>",
			ShortHelp:  "module maintenance",
		},
		commands.NewEmptyConfig(),
		commands.HelpExec,
	)

	cmd.AddSubCommands(
		newModDownloadCmd(io),
		// edit
		newModGraphCmd(io),
		newModInitCmd(),
		newModTidy(io),
		// vendor
		// verify
		newModWhy(io),
	)

	return cmd
}

func newModDownloadCmd(io commands.IO) *commands.Command {
	cfg := &modDownloadCfg{}

	return commands.NewCommand(
		commands.Metadata{
			Name:       "download",
			ShortUsage: "download [flags]",
			ShortHelp:  "download modules to local cache",
		},
		cfg,
		func(_ context.Context, args []string) error {
			return execModDownload(cfg, args, io)
		},
	)
}

func newModGraphCmd(io commands.IO) *commands.Command {
	cfg := &modGraphCfg{}
	return commands.NewCommand(
		commands.Metadata{
			Name:       "graph",
			ShortUsage: "graph [path]",
			ShortHelp:  "print module requirement graph",
		},
		cfg,
		func(_ context.Context, args []string) error {
			return execModGraph(cfg, args, io)
		},
	)
}

func newModInitCmd() *commands.Command {
	return commands.NewCommand(
		commands.Metadata{
			Name:       "init",
			ShortUsage: "init <module-path>",
			ShortHelp:  "initialize gno.mod file in current directory",
		},
		commands.NewEmptyConfig(),
		func(_ context.Context, args []string) error {
			return execModInit(args)
		},
	)
}

func newModTidy(io commands.IO) *commands.Command {
	cfg := &modTidyCfg{}

	return commands.NewCommand(
		commands.Metadata{
			Name:       "tidy",
			ShortUsage: "tidy [flags]",
			ShortHelp:  "add missing and remove unused modules",
		},
		cfg,
		func(_ context.Context, args []string) error {
			return execModTidy(cfg, args, io)
		},
	)
}

func newModWhy(io commands.IO) *commands.Command {
	return commands.NewCommand(
		commands.Metadata{
			Name:       "why",
			ShortUsage: "why <package> [<package>...]",
			ShortHelp:  "Explains why modules are needed",
			LongHelp: `Explains why modules are needed.

gno mod why shows a list of files where specified packages or modules are
being used, explaining why those specified packages or modules are being
kept by gno mod tidy.

The output is a sequence of stanzas, one for each module/package name
specified, separated by blank lines. Each stanza begins with a
comment line "# module" giving the target module/package. Subsequent lines
show files that import the specified module/package, one filename per line.
If the package or module is not being used/needed/imported, the stanza
will display a single parenthesized note indicating that fact.

For example:

	$ gno mod why gno.land/p/demo/avl gno.land/p/demo/users
	# gno.land/p/demo/avl
	[FILENAME_1.gno]
	[FILENAME_2.gno]

	# gno.land/p/demo/users
	(module [MODULE_NAME] does not need package gno.land/p/demo/users)
	$
`,
		},
		commands.NewEmptyConfig(),
		func(_ context.Context, args []string) error {
			return execModWhy(args, io)
		},
	)
}

type modDownloadCfg struct {
	remoteOverrides string
}

const remoteOverridesArgName = "remote-overrides"

func (c *modDownloadCfg) RegisterFlags(fs *flag.FlagSet) {
	fs.StringVar(
		&c.remoteOverrides,
		remoteOverridesArgName,
		"",
		"chain-domain=rpc-url comma-separated list",
	)
}

type modGraphCfg struct{}

func (c *modGraphCfg) RegisterFlags(fs *flag.FlagSet) {
	// /out std
	// /out remote
	// /out _test processing
	// ...
}

func execModGraph(cfg *modGraphCfg, args []string, io commands.IO) error {
	// default to current directory if no args provided
	if len(args) == 0 {
		args = []string{"."}
	}
	if len(args) > 1 {
		return flag.ErrHelp
	}

	stdout := io.Out()

	pkgs, err := gno.ReadPkgListFromDir(args[0], gno.MPAnyAll)
	if err != nil {
		return err
	}
	for _, pkg := range pkgs {
		for _, dep := range pkg.Imports {
			fmt.Fprintf(stdout, "%s %s\n", pkg.Name, dep)
		}
	}
	return nil
}

func execModDownload(cfg *modDownloadCfg, args []string, io commands.IO) error {
	if len(args) > 0 {
		return flag.ErrHelp
	}

	fetcher := testPackageFetcher
	if fetcher == nil {
		remoteOverrides, err := parseRemoteOverrides(cfg.remoteOverrides)
		if err != nil {
			return fmt.Errorf("invalid %s flag: %w", remoteOverridesArgName, err)
		}
		fetcher = rpcpkgfetcher.New(remoteOverrides)
	} else if len(cfg.remoteOverrides) != 0 {
		return fmt.Errorf("can't use %s flag with a custom package fetcher", remoteOverridesArgName)
	}

	path, err := os.Getwd()
	if err != nil {
		return err
	}

	gnoMod, err := gnomod.ParseDir(path)
	if err != nil {
		return err
	}

	// sanitize gno.mod
	gnoMod.Sanitize()

	// validate gno.mod
	if err := gnoMod.Validate(); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	if err := downloadDeps(io, path, gnoMod, fetcher, make(map[string]struct{})); err != nil {
		return err
	}

	return nil
}

func parseRemoteOverrides(arg string) (map[string]string, error) {
	pairs := strings.Split(arg, ",")
	res := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		parts := strings.Split(pair, "=")
		if len(parts) != 2 {
			return nil, fmt.Errorf("expected 2 parts in chain-domain=rpc-url pair %q", arg)
		}
		domain := strings.TrimSpace(parts[0])
		rpcURL := strings.TrimSpace(parts[1])
		res[domain] = rpcURL
	}
	return res, nil
}

func execModInit(args []string) error {
	if len(args) > 1 {
		return flag.ErrHelp
	}
	var modPath string
	if len(args) == 1 {
		modPath = args[0]
	}
	rootDir, err := os.Getwd()
	if err != nil {
		return err
	}

	if !filepath.IsAbs(rootDir) {
		return fmt.Errorf("create gnomod.toml: dir %q is not absolute", rootDir)
	}

	modFilePath := filepath.Join(rootDir, "gnomod.toml")
	if _, err := os.Stat(modFilePath); err == nil {
		return errors.New("create gnomod.toml: file already exists")
	}

	if err := module.CheckImportPath(modPath); err != nil {
		return fmt.Errorf("create gnomod.toml: %w", err)
	}

	if !gno.IsUserlib(modPath) {
		return fmt.Errorf("create gnomod.toml: %q is not a valid package path URL", modPath)
	}

	modfile := new(gnomod.File)
	modfile.Module = modPath
	modfile.Gno = gno.GnoVerLatest
	modfile.WriteFile(filepath.Join(rootDir, "gnomod.toml"))

	return nil
}

type modTidyCfg struct {
	verbose   bool
	recursive bool
}

func (c *modTidyCfg) RegisterFlags(fs *flag.FlagSet) {
	fs.BoolVar(
		&c.verbose,
		"v",
		false,
		"verbose output when running",
	)
	fs.BoolVar(
		&c.recursive,
		"recursive",
		false,
		"walk subdirs for gno.mod files",
	)
}

func execModTidy(cfg *modTidyCfg, args []string, io commands.IO) error {
	if len(args) > 0 {
		return flag.ErrHelp
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	if cfg.recursive {
		pkgs, err := gno.ReadPkgListFromDir(wd, gno.MPAnyAll)
		if err != nil {
			return err
		}
		var errs error
		for _, pkg := range pkgs {
			err := modTidyOnce(cfg, wd, pkg.Dir, io)
			errs = multierr.Append(errs, err)
		}
		return errs
	}

	// XXX: recursively check parents if no $PWD/gno.mod
	return modTidyOnce(cfg, wd, wd, io)
}

func modTidyOnce(cfg *modTidyCfg, wd, pkgdir string, io commands.IO) error {
	modExists := false
	for _, fname := range []string{"gnomod.toml", "gno.mod"} {
		fpath := filepath.Join(pkgdir, fname)
		if !isFileExist(fpath) {
			continue
		}

		modExists = true

		relpath, err := filepath.Rel(wd, fpath)
		if err != nil {
			return err
		}
		if cfg.verbose {
			io.ErrPrintfln("%s", relpath)
		}

		gm, err := gnomod.ParseFilepath(fpath)
		if err != nil {
			return err
		}

		if fname == "gno.mod" {
			// migrate from gno.mod to gnomod.toml
			newPath := filepath.Join(pkgdir, "gnomod.toml")
			gm.WriteFile(newPath) // gnomod.toml
		} else {
			gm.WriteFile(fpath) // gnomod.toml
		}
	}

	// there is no gno.mod nor gnomod.toml
	if !modExists {
		return gnomod.ErrNoModFile
	}

	// remove gno.mod if it exists.
	oldpath := filepath.Join(pkgdir, "gno.mod")
	os.Remove(oldpath)

	return nil
}

func execModWhy(args []string, io commands.IO) error {
	if len(args) < 1 {
		return flag.ErrHelp
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	gm, err := gnomod.ParseDir(wd)
	if err != nil {
		return err
	}

	importToFilesMap, err := getImportToFilesMap(wd)
	if err != nil {
		return err
	}

	// Format and print `gno mod why` output stanzas
	out := formatModWhyStanzas(gm.Module, args, importToFilesMap)
	io.Printf(out)

	return nil
}

// formatModWhyStanzas returns a formatted output for the go mod why command.
// It takes three parameters:
//   - modulePath (the path of the module)
//   - args (input arguments)
//   - importToFilesMap (a map of import to files).
func formatModWhyStanzas(modulePath string, args []string, importToFilesMap map[string][]string) (out string) {
	for i, arg := range args {
		out += fmt.Sprintf("# %s\n", arg)
		files, ok := importToFilesMap[arg]
		if !ok {
			out += fmt.Sprintf("(module %s does not need package %s)\n", modulePath, arg)
		} else {
			for _, file := range files {
				out += file + "\n"
			}
		}
		if i < len(args)-1 { // Add a newline if it's not the last stanza
			out += "\n"
		}
	}
	return
}

// getImportToFilesMap returns a map where each key is an import path and its
// value is a list of files importing that package with the specified import path.
func getImportToFilesMap(pkgPath string) (map[string][]string, error) {
	entries, err := os.ReadDir(pkgPath)
	if err != nil {
		return nil, err
	}
	m := make(map[string][]string) // import -> []file
	for _, e := range entries {
		filename := e.Name()
		if ext := filepath.Ext(filename); ext != ".gno" {
			continue
		}
		if strings.HasSuffix(filename, "_filetest.gno") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(pkgPath, filename))
		if err != nil {
			return nil, err
		}
		imports, err := packages.FileImports(filename, string(data), nil)
		if err != nil {
			return nil, err
		}

		for _, imp := range imports {
			m[imp.PkgPath] = append(m[imp.PkgPath], filename)
		}
	}
	return m, nil
}

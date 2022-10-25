package repomgr

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Masterminds/semver/v3"
	"github.com/kazhuravlev/just"
	"github.com/mitchellh/mapstructure"
	"github.com/olekukonko/tablewriter"
	"golang.org/x/mod/modfile"
	"gopkg.in/yaml.v3"
)

func New(opts Options) (*RepoManager, error) {
	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("invalid options: %w", err)
	}

	return &RepoManager{opts: opts}, nil
}

func (m *RepoManager) Run() error {
	if err := m.init(); err != nil {
		return fmt.Errorf("cannot init repo manager: %w", err)
	}

	reports, err := m.handleRepos()
	if err != nil {
		return fmt.Errorf("cannot handle repos: %w", err)
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Repo", "Warn"})
	table.SetAutoWrapText(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetTablePadding("\t") // pad with tabs
	table.SetNoWhiteSpace(true)

	just.SliceApply(
		just.SliceMap(
			reports,
			func(r RepoReport) [][]string {
				return just.SliceMap(r.Warnings, func(w string) []string {
					return []string{r.RepoSpec.Name, w}
				})
			},
		),
		func(_ int, rows [][]string) {
			table.AppendBulk(rows)
		})

	if table.NumLines() != 0 {
		table.Render()
		os.Exit(1)
	}

	return nil
}

func (m *RepoManager) initPolicy(spec PolicySpec) (Rule, error) {
	var rules []Rule
	for i := range spec.Rules {
		rs := spec.Rules[i]
		rule, err := m.initRule(rs)
		if err != nil {
			return nil, fmt.Errorf("cannot init rule `%s`: %w", rs.Rule, err)
		}

		rules = append(rules, rule)
	}

	return func(repo Repo) []string {
		var warnings []string
		for i := range rules {
			warnings = append(warnings, rules[i](repo)...)
		}

		return warnings
	}, nil
}

func (m *RepoManager) initRule(spec RuleSpec) (Rule, error) {
	switch spec.Rule {
	default:
		return nil, fmt.Errorf("unknown rule: %s", spec.Rule)
	case RuleNameGoDepModMinVersion:
		var req GoDepModMinVersionReq
		if err := mapstructure.WeakDecode(spec.Params, &req); err != nil {
			return nil, fmt.Errorf("cannot parse rule params: %w", err)
		}

		if req.MinVersion == "latest" {
			minVersion, err := fetchLastTag(req.Module, m.opts.privateKey)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch last tag: %w", err)
			}

			req.MinVersion = minVersion
		}

		return ruleMustPresentRequiredVersionGte(req)
	case RuleNameGoDepHasNoModule:
		var req GoDepHasNoModuleReq
		if err := mapstructure.WeakDecode(spec.Params, &req); err != nil {
			return nil, fmt.Errorf("cannot parse rule params: %w", err)
		}

		return ruleGoDepHasNoModule(req), nil
	case RuleNameGoVersion:
		var req GoVersion
		if err := mapstructure.WeakDecode(spec.Params, &req); err != nil {
			return nil, fmt.Errorf("cannot parse rule params: %w", err)
		}

		return ruleGoVersion(req)
	}
}

type RepoManager struct {
	opts Options

	policies map[string]Rule
}

type RepoReport struct {
	RepoSpec RepoSpec
	Repo     Repo
	Warnings []string
}

func (s *RepoManager) init() error {
	policies := make(map[string]Rule)
	for i := range s.opts.spec.Policies {
		policySpec := s.opts.spec.Policies[i]

		policyRule, err := s.initPolicy(policySpec)
		if err != nil {
			return fmt.Errorf("cannot init policy: %w", err)
		}

		policies[policySpec.ID] = policyRule
	}

	s.policies = policies

	return nil
}

func (s *RepoManager) handleRepos() ([]RepoReport, error) {
	var reports []RepoReport
	for i := range s.opts.spec.Repos {
		repoSpec := s.opts.spec.Repos[i]

		switch repoSpec.Type {
		default:
			return nil, fmt.Errorf("unknown repo type: %s", repoSpec.Type)
		case "golang":
			repo, err := parseGolangRepo(repoSpec.Path)
			if err != nil {
				return nil, fmt.Errorf("parse golang repo: %w", err)
			}

			warnings, err := s.handleGolangRepo(repoSpec, *repo)
			if err != nil {
				return nil, fmt.Errorf("cannot handle golang repo: %w", err)
			}

			reports = append(reports, RepoReport{
				RepoSpec: repoSpec,
				Repo:     *repo,
				Warnings: warnings,
			})
		}
	}

	return reports, nil
}

func (s *RepoManager) handleGolangRepo(rs RepoSpec, r Repo) ([]string, error) {
	var warnings []string
	for i := range rs.Policies {
		policyFn, ok := s.policies[rs.Policies[i]]
		if !ok {
			return nil, fmt.Errorf("unknown policy: %s", rs.Policies[i])
		}

		warnings = append(warnings, policyFn(r)...)
	}

	return warnings, nil
}

type Repo struct {
	AbsPath       string
	GoModFilename string
	GoModFile     modfile.File
}

func parseGolangRepo(path string) (*Repo, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("make repo path absolute: %w", err)
	}

	goModFilename := filepath.Join(path, "go.mod")
	goModBody, err := os.ReadFile(goModFilename)
	if err != nil {
		return nil, fmt.Errorf("read go.mod file: %w", err)
	}

	goModFile, err := modfile.Parse(goModFilename, goModBody, nil)
	if err != nil {
		return nil, fmt.Errorf("parse go.mod file: %w", err)
	}

	return &Repo{
		AbsPath:       path,
		GoModFilename: goModFilename,
		GoModFile:     *goModFile,
	}, nil
}

type Rule func(Repo) []string

func ruleMustPresentRequire(module string) Rule {
	warnReqMustPresent := fmt.Sprintf("must: requirement `%s` is present in go.mod", module)

	return func(r Repo) []string {
		actualReq := just.SliceFindFirst(r.GoModFile.Require, func(_ int, requirement *modfile.Require) bool {
			return requirement.Mod.Path == module
		})
		if !actualReq.Ok() {
			return []string{warnReqMustPresent}
		}

		return nil
	}
}

func ruleMustPresentRequireVersion(module string, version *semver.Version) Rule {
	warnReqMustPresent := fmt.Sprintf("must: requirement `%s` is present in go.mod", module)
	warnReqMustHasConcreteVersion := fmt.Sprintf("must: requirement `%s` have version `%s` go.mod", module, version.String())

	return func(r Repo) []string {
		actualReq := just.SliceFindFirst(r.GoModFile.Require, func(_ int, requirement *modfile.Require) bool {
			return requirement.Mod.Path == module
		})
		if !actualReq.Ok() {
			return []string{warnReqMustPresent}
		}

		modVersion, err := semver.NewVersion(actualReq.Val.Mod.Version)
		if err != nil {
			return []string{warnReqMustHasConcreteVersion}
		}

		if !modVersion.Equal(version) {
			return []string{warnReqMustHasConcreteVersion}
		}

		return nil
	}
}

type GoDepModMinVersionReq struct {
	Module     string
	MinVersion string
}

func ruleMustPresentRequiredVersionGte(req GoDepModMinVersionReq) (Rule, error) {
	minVersion, err := semver.NewVersion(req.MinVersion)
	if err != nil {
		return nil, fmt.Errorf("bad version format: %w", err)
	}

	warnReqMustPresent := fmt.Sprintf("must: requirement `%s` is present in go.mod", req.Module)
	warnReqMustHasVersionAtLeast := fmt.Sprintf("must: requirement `%s` with AT LEAST this version `%s` is present in go.mod", req.Module, minVersion.String())

	return func(r Repo) []string {
		actualReq := just.SliceFindFirst(r.GoModFile.Require, func(_ int, requirement *modfile.Require) bool {
			return requirement.Mod.Path == req.Module
		})
		if !actualReq.Ok() {
			return []string{warnReqMustPresent}
		}

		modVersion, err := semver.NewVersion(actualReq.Val.Mod.Version)
		if err != nil {
			return []string{warnReqMustHasVersionAtLeast}
		}

		if !(modVersion.GreaterThan(minVersion) || modVersion.Equal(minVersion)) {
			return []string{warnReqMustHasVersionAtLeast}
		}

		return nil
	}, nil
}

type GoDepHasNoModuleReq struct {
	Module string
}

func ruleGoDepHasNoModule(req GoDepHasNoModuleReq) Rule {
	warnReqShouldNotUsed := fmt.Sprintf("must: requirement `%s` not used", req.Module)

	return func(r Repo) []string {
		actualReq := just.SliceFindFirst(r.GoModFile.Require, func(_ int, requirement *modfile.Require) bool {
			return requirement.Mod.Path == req.Module
		})
		if actualReq.Ok() {
			return []string{warnReqShouldNotUsed}
		}

		return nil
	}
}

type GoVersion struct {
	MinVersion string
}

func ruleGoVersion(req GoVersion) (Rule, error) {
	warnGoVersionIsTooOld := fmt.Sprintf("must: golang version at least `%s`", req.MinVersion)
	minVersion, err := semver.NewVersion(req.MinVersion)
	if err != nil {
		return nil, fmt.Errorf("cannot parse min version: %w", err)
	}

	return func(r Repo) []string {
		actualVersion, err := semver.NewVersion(r.GoModFile.Go.Version)
		if err != nil {
			return []string{warnGoVersionIsTooOld}
		}

		if actualVersion.LessThan(minVersion) {
			return []string{warnGoVersionIsTooOld}
		}

		return nil
	}, nil
}

type RepoSpec struct {
	Name     string
	Path     string
	Type     string
	Policies []string
}

type RuleName string

const (
	RuleNameGoDepModMinVersion RuleName = "go-dep-module-min-version"
	RuleNameGoDepHasNoModule   RuleName = "go-dep-has-no-module"
	RuleNameGoVersion          RuleName = "go-version"
)

type RuleSpec struct {
	Rule   RuleName
	Params any
}

type PolicySpec struct {
	ID    string
	Name  string
	Rules []RuleSpec
}

type Spec struct {
	Version  string
	Policies []PolicySpec
	Repos    []RepoSpec
}

func ParseSpec(filename string) (*Spec, error) {
	body, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot read file: %w", err)
	}

	var spec Spec
	if err := yaml.Unmarshal(body, &spec); err != nil {
		return nil, fmt.Errorf("cannot unmarshal spec: %w", err)
	}

	if spec.Version != "1" {
		return nil, errors.New("unknown spec version")
	}

	return &spec, nil
}

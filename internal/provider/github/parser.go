package github

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

    "github.com/bgricker/testdrive/internal/provider"
	"gopkg.in/yaml.v3"
)

const ProviderName = "github"

// Parser loads GitHub Actions workflow files from disk.
type Parser struct {
	Root string
}

// NewParser constructs a Parser that resolves workflow paths relative to root.
func NewParser(root string) *Parser {
	return &Parser{Root: root}
}

// Parse reads the supplied workflow paths and produces a Pipeline data model.
func (p *Parser) Parse(paths []string) (provider.Pipeline, error) {
	pipeline := provider.Pipeline{Provider: ProviderName}
	for _, relPath := range paths {
		full := relPath
		if !filepath.IsAbs(full) {
			full = filepath.Join(p.Root, relPath)
		}
		wf, warnings, err := parseWorkflow(full, relPath)
		if err != nil {
			return provider.Pipeline{}, err
		}
		pipeline.Workflows = append(pipeline.Workflows, wf)
		pipeline.Warnings = append(pipeline.Warnings, warnings...)
	}
	return pipeline, nil
}

func parseWorkflow(fullPath, displayPath string) (provider.Workflow, []provider.Warning, error) {
	f, err := os.Open(fullPath)
	if err != nil {
		return provider.Workflow{}, nil, fmt.Errorf("open workflow %q: %w", displayPath, err)
	}
	defer f.Close()
	return decodeWorkflow(f, displayPath)
}

func decodeWorkflow(r io.Reader, displayPath string) (provider.Workflow, []provider.Warning, error) {
	decoder := yaml.NewDecoder(r)

	var wfDoc workflowDocument
	if err := decoder.Decode(&wfDoc); err != nil {
		return provider.Workflow{}, nil, fmt.Errorf("parse workflow %q: %w", displayPath, err)
	}

	wf := provider.Workflow{
		Path: displayPath,
		Name: wfDoc.Name,
		Env:  convertEnv(wfDoc.Env),
		Defaults: provider.Defaults{
			RunShell:         wfDoc.Defaults.Run.Shell,
			WorkingDirectory: wfDoc.Defaults.Run.WorkingDirectory,
		},
	}

	if wf.Name == "" {
		wf.Name = filepath.Base(displayPath)
	}

	warnings := make([]provider.Warning, 0)

	jobIDs := make([]string, 0, len(wfDoc.Jobs))
	for id := range wfDoc.Jobs {
		jobIDs = append(jobIDs, id)
	}
	sort.Strings(jobIDs)

	wf.Jobs = make([]provider.Job, 0, len(jobIDs))
	for _, jobID := range jobIDs {
		jobDoc := wfDoc.Jobs[jobID]
		job := provider.Job{
			RawID: jobID,
			Name:  jobDoc.Name,
			Env:   convertEnv(jobDoc.Env),
			Defaults: provider.Defaults{
				RunShell:         jobDoc.Defaults.Run.Shell,
				WorkingDirectory: jobDoc.Defaults.Run.WorkingDirectory,
			},
		}
		if job.Name == "" {
			job.Name = jobID
		}

		if jobDoc.Services != nil {
			job.HasServices = true
			warnings = append(warnings, provider.Warning{
				Workflow: displayPath,
				Job:      jobID,
				Message:  "services are not supported; automatically using local environment instead of workflow env",
			})
		}
		if jobDoc.Strategy.Matrix != nil {
			warnings = append(warnings, provider.Warning{
				Workflow: displayPath,
				Job:      jobID,
				Message:  "strategy.matrix is not supported",
			})
		}
		if jobDoc.If != "" {
			warnings = append(warnings, provider.Warning{
				Workflow: displayPath,
				Job:      jobID,
				Message:  "job-level if condition is ignored",
			})
		}

		job.Steps = make([]provider.Step, 0, len(jobDoc.Steps))
		for idx, stepDoc := range jobDoc.Steps {
			step := provider.Step{
				Name:             stepDoc.Name,
				Run:              stepDoc.Run,
				Uses:             stepDoc.Uses,
				Env:              convertEnv(stepDoc.Env),
				Shell:            stepDoc.Shell,
				WorkingDirectory: stepDoc.WorkingDirectory,
			}
			if step.Name == "" {
				step.Name = fmt.Sprintf("step %d", idx+1)
			}
			if stepDoc.If != "" {
				warnings = append(warnings, provider.Warning{
					Workflow: displayPath,
					Job:      jobID,
					Message:  fmt.Sprintf("step %q has unsupported if condition", step.Name),
				})
			}
			job.Steps = append(job.Steps, step)
		}

		wf.Jobs = append(wf.Jobs, job)
	}

	return wf, warnings, nil
}

type workflowDocument struct {
	Name     string                 `yaml:"name"`
	Env      map[string]interface{} `yaml:"env"`
	Defaults defaultsDocument       `yaml:"defaults"`
	Jobs     map[string]jobDocument `yaml:"jobs"`
}

type defaultsDocument struct {
	Run runDefaults `yaml:"run"`
}

type runDefaults struct {
	Shell            string `yaml:"shell"`
	WorkingDirectory string `yaml:"working-directory"`
}

type jobDocument struct {
	Name     string                 `yaml:"name"`
	Env      map[string]interface{} `yaml:"env"`
	Defaults defaultsDocument       `yaml:"defaults"`
	Steps    []stepDocument         `yaml:"steps"`
	Services interface{}            `yaml:"services"`
	Strategy strategyDocument       `yaml:"strategy"`
	If       string                 `yaml:"if"`
}

type strategyDocument struct {
	Matrix interface{} `yaml:"matrix"`
}

type stepDocument struct {
	Name             string                 `yaml:"name"`
	Run              string                 `yaml:"run"`
	Uses             string                 `yaml:"uses"`
	Env              map[string]interface{} `yaml:"env"`
	Shell            string                 `yaml:"shell"`
	WorkingDirectory string                 `yaml:"working-directory"`
	If               string                 `yaml:"if"`
}

func convertEnv(input map[string]interface{}) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out[k] = fmt.Sprint(input[k])
	}
	return out
}

package provider

// Pipeline represents a parsed set of workflows from a provider.
type Pipeline struct {
	Provider  string     `json:"provider"`
	Workflows []Workflow `json:"workflows"`
	Warnings  []Warning  `json:"warnings"`
}

// Warning captures non-fatal issues encountered while parsing workflows.
type Warning struct {
	Workflow string `json:"workflow"`
	Job      string `json:"job"`
	Message  string `json:"message"`
}

// Workflow mirrors a GitHub Actions workflow file.
type Workflow struct {
	Path     string            `json:"path"`
	Name     string            `json:"name"`
	Env      map[string]string `json:"env,omitempty"`
	Defaults Defaults          `json:"defaults"`
	Jobs     []Job             `json:"jobs"`
}

// Defaults capture shared configuration for jobs and steps.
type Defaults struct {
	RunShell         string `json:"run_shell,omitempty"`
	WorkingDirectory string `json:"working_directory,omitempty"`
}

// Job represents a GitHub Actions job with resolved steps.
type Job struct {
	Name        string            `json:"name"`
	RawID       string            `json:"id"`
	Env         map[string]string `json:"env,omitempty"`
	Defaults    Defaults          `json:"defaults"`
	Steps       []Step            `json:"steps"`
	HasServices bool              `json:"has_services,omitempty"` // True if job defines services (which testdrive can't run)
}

// Step represents an individual GitHub Actions workflow step.
type Step struct {
	Name             string            `json:"name"`
	Run              string            `json:"run,omitempty"`
	Uses             string            `json:"uses,omitempty"`
	Shell            string            `json:"shell,omitempty"`
	WorkingDirectory string            `json:"working_directory,omitempty"`
	Env              map[string]string `json:"env,omitempty"`
}

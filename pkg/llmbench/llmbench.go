// Package llmbench provides a unified API for the LLM benchmark suite.
//
// It re-exports key types from sub-packages so consumers can import
// a single package for common operations.
package llmbench

import (
	"github.com/lazypower/spark-tools/pkg/llmbench/config"
	"github.com/lazypower/spark-tools/pkg/llmbench/job"
	"github.com/lazypower/spark-tools/pkg/llmbench/metrics"
	"github.com/lazypower/spark-tools/pkg/llmbench/prompts"
	"github.com/lazypower/spark-tools/pkg/llmbench/report"
	"github.com/lazypower/spark-tools/pkg/llmbench/store"
	"github.com/lazypower/spark-tools/pkg/llmbench/suite"
	"github.com/lazypower/spark-tools/pkg/llmbench/syscheck"
)

// Suite types.
type (
	BenchmarkSuite = suite.BenchmarkSuite
	JobDefaults    = suite.JobDefaults
	ModelSpec      = suite.ModelSpec
	Scenario       = suite.Scenario
	PromptSet      = suite.PromptSet
	SuiteSettings  = suite.SuiteSettings
	JobSpec        = suite.JobSpec
	Duration       = suite.Duration
)

// Job types.
type (
	JobResult = job.JobResult
	JobStatus = job.JobStatus
	JobError  = job.JobError
)

// Metrics types.
type (
	ThroughputStats = metrics.ThroughputStats
	SystemMetrics   = metrics.SystemMetrics
	RawSample       = metrics.RawSample
)

// Store types.
type (
	RunResult  = store.RunResult
	RunSummary = store.RunSummary
)

// Config types.
type (
	DirConfig = config.DirConfig
)

// Suite operations.
var (
	LoadSuite  = suite.LoadSuite
	ParseSuite = suite.ParseSuite
	ExpandJobs = suite.ExpandJobs
	FilterJobs = suite.FilterJobs
	ScenarioID = suite.ScenarioID
)

// Runner operations.
var (
	NewRunner      = suite.NewRunner
	WithEngine     = suite.WithEngine
	WithStore      = suite.WithStore
	WithOutputDir  = suite.WithOutputDir
	WithProgressFunc = suite.WithProgressFunc
	WithSkipCheck  = suite.WithSkipCheck
	WithDirtyMode  = suite.WithDirtyMode
	WithContinueFrom = suite.WithContinueFrom
	WithJobFilter  = suite.WithJobFilter
)

// Prompt operations.
var (
	LoadBuiltin  = prompts.LoadBuiltin
	BuiltinSets  = prompts.BuiltinSets
	LoadFile     = prompts.LoadFile
)

// Report operations.
var (
	ReportTerminal = report.Terminal
	ReportJSON     = report.JSON
	ReportJSONPretty = report.JSONPretty
	ReportCSV      = report.CSV
	ReportCompare  = report.Compare
	ReportQuick    = report.QuickResult
)

// Store operations.
var (
	NewStore       = store.NewStore
	GenerateRunID  = store.GenerateRunID
)

// Config operations.
var (
	Dirs = config.Dirs
)

// Syscheck operations.
var (
	RunPreflight   = syscheck.RunPreflight
	ParseDirtyMode = syscheck.ParseDirtyMode
)

// Constants.
const (
	JobStatusOK      = job.JobStatusOK
	JobStatusFailed  = job.JobStatusFailed
	JobStatusSkipped = job.JobStatusSkipped
)

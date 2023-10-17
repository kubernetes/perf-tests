/*
Copyright 2023 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package common

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/exec"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/informer"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	podPeriodicCommandMeasurementName = "PodPeriodicCommand"
)

type podPeriodicCommandMeasurementCommandParams struct {
	// Name is an identifier for the command.
	Name string
	// Command is the actual Command to execute in a pod.
	Command []string
	// Timeout is the maximum amount of time the command will have to finish.
	Timeout time.Duration
}

type podPeriodicCommandMeasurementParams struct {
	// LabelSelector is used to select applicable pods to run commands on.
	LabelSelector *labels.Selector
	// Interval is the time between sequential command executions.
	Interval time.Duration
	// Container is the name of the Container to run the command in.
	Container string
	// Limit is the maximum number of pods that will have the commands executed in on every interval.
	Limit int
	// FailOnCommandError controls if the measurement will fail if a command has a non-zero RC during the life of the measurement.
	FailOnCommandError bool
	// FailOnExecError controls if the measurement will fail if an error occurs while trying to execute a command.
	// For example, this would include any error returned from the k8s client-go library.
	FailOnExecError bool
	// FailOnTimeout controls if the measurement will fail if a command times out.
	FailOnTimeout bool
	// Commands is the list of Commands that will be executed in each pod on each interval.
	Commands []*podPeriodicCommandMeasurementCommandParams
}

func newPodPeriodCommandMeasurementParams(
	params map[string]interface{},
) (p *podPeriodicCommandMeasurementParams, err error) {
	p = &podPeriodicCommandMeasurementParams{}

	p.LabelSelector, err = util.GetLabelSelector(params, "labelSelector")
	if err != nil {
		return
	}
	p.Interval, err = util.GetDuration(params, "interval")
	if err != nil {
		return
	}

	p.Container, err = util.GetString(params, "container")
	if err != nil {
		return
	}

	p.Limit, err = util.GetInt(params, "limit")
	if err != nil {
		return
	}

	p.FailOnCommandError, err = util.GetBool(params, "failOnCommandError")
	if err != nil {
		return
	}

	p.FailOnExecError, err = util.GetBool(params, "failOnExecError")
	if err != nil {
		return
	}

	p.FailOnTimeout, err = util.GetBool(params, "failOnTimeout")
	if err != nil {
		return
	}

	var commandMaps []map[string]interface{}
	commandMaps, err = util.GetMapArray(params, "commands")
	if err != nil {
		return
	}

	p.Commands = []*podPeriodicCommandMeasurementCommandParams{}
	for _, commandMap := range commandMaps {
		c := &podPeriodicCommandMeasurementCommandParams{}

		c.Name, err = util.GetString(commandMap, "name")
		if err != nil {
			return
		}

		c.Command, err = util.GetStringArray(commandMap, "command")
		if err != nil {
			return
		}

		c.Timeout, err = util.GetDuration(commandMap, "timeout")
		if err != nil {
			return
		}

		p.Commands = append(p.Commands, c)
	}

	return p, nil
}

type runCommandResult struct {
	// stdout is the saved stdout from the command. Will be stored as its own measurement summary.
	stdout string
	// stderr is the saved stderr from the command. Will be stored as its own measurement summary.
	stderr string
	// ExitCode is the RC from the command. Defaults to zero and will not be set if the command times
	// out or fails to run.
	ExitCode int `json:"exitCode"`
	// Name is the name of the command that was run, set in the config.
	Name string `json:"name"`
	// Command is the actual command which was executed.
	Command []string `json:"command"`
	// Timeout is the configured timeout duration.
	Timeout string `json:"timeout"`
	// HitTimeout is set to true if the command did not finish before the timeout.
	HitTimeout bool `json:"hitTimeout"`
	// StartTime is the time the command began executing. Isn't super precise.
	StartTime time.Time `json:"startTime"`
	// EndTime is the time the command finished executing. Isn't super precise.
	EndTime time.Time `json:"endTime"`
	// ExecError is set to any go error raised while executing the command.
	ExecError string `json:"execError"`
}

type runAllCommandsResult struct {
	Pod       string              `json:"pod"`
	Namespace string              `json:"namespace"`
	Container string              `json:"container"`
	Commands  []*runCommandResult `json:"commands"`
}

type stats struct {
	// Execs is the total number of times a command was executed in a pod.
	Execs int `json:"execs"`
	// ExecErrors is the total number of errors that were observed, not including errors from the executed commands.
	// For example, this includes any errors that are returned by the k8s client-go library.
	ExecErrors int `json:"execErrors"`
	// Timeouts is the number of commands which hit a timeout.
	Timeouts int `json:"timeouts"`
	// NonZeroRCs is the total number of non-zero return codes that were collected from the commands executed.
	NonZeroRCs int `json:"nonZeroRCs"`
	// Measurements is the total number of measurements gathered.
	Measurements int `json:"measurements"`
	// Ticks is the total number of intervals that were executed.
	Ticks int `json:"ticks"`
	// TicksNoPods is the total number of intervals that were skipped because no applicable pods could be found.
	TicksNoPods int `json:"ticksNoPods"`
}

// podPeriodicCommandMeasurement can be used to continually run commands within pods at an interval.
//
// It works by performing the following on each tick:
//
//  1. Creating a list of pods, with maximum size `params.Limit`, which will execute the configured commands.
//     Pods are selected using `params.LabelSelector`, must contain `params.Container`, and must be in a running
//     state. If no applicable pods are available, then no step is performed for the tick.
//  2. For each pod, spin a goroutine which will run all configured commands in the pod.
//  3. For each command, spin a goroutine to handle running the command.
//  4. If the command returns non-zero, this will be reflected in the associated measurement.
//  5. If a go error occurred while trying to execute the command, this will be reflected in the associated measurement.
//
// The following measurements are produced during the gather step:
//
//  1. One summary measurement, which includes information on all executed commands, such as if the command
//     took longer than `params.Timeout`, the command's RC, and the pod the command was executed on.
//  2. One measurement for each command's non-empty stdout and stderr.
//  3. One measurement containing statistics, such as the number of commands executed, the number of errors observed,
//     and the number of non-zero RCs.
//
// The measurement fails in the following scenarios:
//
//  1. `params.FailOnCommandError` is set to true and a command has a non-zero RC.
//  2. `params.FailOnExecError` is set to true and an error occurs while trying to execute a command.
//  3. `params.FailOnTimeout` is set to true and a command takes longer than its configured timeout to execute.
type podPeriodicCommandMeasurement struct {
	clientset  kubernetes.Interface
	restConfig *rest.Config
	params     *podPeriodicCommandMeasurementParams
	isRunning  bool
	// skipGather signals if the gather step should be skipped, mainly used to bail if param parsing failed.
	skipGather bool
	// stopCh is closed when stop() is called.
	stopCh chan struct{}
	// doneCh is closed after stopCh is closed and all in progress commands have finished.
	doneCh   chan struct{}
	results  []*runAllCommandsResult
	informer cache.SharedInformer
	stats    *stats
	// statsLock needs to be held to modify and read the stats field.
	statsLock *sync.Mutex
}

// isApplicablePod checks if a pod is a viable candidate for running a command on.
func (p *podPeriodicCommandMeasurement) isApplicablePod(pod *v1.Pod) bool {
	if pod.Status.Phase != v1.PodRunning {
		return false
	}

	hasContainer := false
	for _, c := range pod.Spec.Containers {
		if c.Name == p.params.Container {
			hasContainer = true

			break
		}
	}

	if !hasContainer {
		return false
	}

	for _, c := range pod.Status.Conditions {
		if c.Type == v1.PodReady && c.Status == v1.ConditionTrue {
			return true
		}
	}

	return false
}

// getMaxNPods gets at most N pods from the internal informer's store.
// The informer uses a ThreadSafeStore, which stores objects in a map. When List is called, the map is
// iterated over using range, which ensures a random order.
func (p *podPeriodicCommandMeasurement) getMaxNPods(n int) []*v1.Pod {
	store := p.informer.GetStore()
	pods := []*v1.Pod{}

	podList := store.List()
	if len(podList) == 0 {
		return pods
	}

	for _, podInterface := range podList {
		pod := podInterface.(*v1.Pod)
		if !p.isApplicablePod(pod) {
			continue
		}

		pods = append(pods, pod)

		if len(pods) >= n {
			return pods
		}
	}

	return pods
}

// runCommandInPod runs a specific given command in the specific given pod.
func (p *podPeriodicCommandMeasurement) runCommandInPod(
	pod *v1.Pod, params *podPeriodicCommandMeasurementCommandParams,
) *runCommandResult {
	klog.V(4).Infof(
		"%s: running named command %s in pod %s/%s",
		podPeriodicCommandMeasurementName, params.Name, pod.Namespace, pod.Name,
	)

	p.statsLock.Lock()
	p.stats.Execs++
	p.statsLock.Unlock()

	result := &runCommandResult{
		Name:       params.Name,
		Command:    params.Command,
		Timeout:    params.Timeout.String(),
		ExitCode:   0,
		HitTimeout: false,
	}

	req := p.clientset.CoreV1().RESTClient().
		Post().
		Namespace(pod.Namespace).
		Resource("pods").
		Name(pod.Name).
		SubResource("exec").
		VersionedParams(&v1.PodExecOptions{
			Container: p.params.Container,
			Command:   params.Command,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(p.restConfig, "POST", req.URL())
	if err != nil {
		result.ExecError = err.Error()

		return result
	}

	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}
	// Holds error returned from executor.Stream.
	execErrChan := make(chan error, 1)

	// The logic used here to start the executor and the timeout timer isn't super precise, but
	// it is good enough for this use case. It is ok that the timeout timer is started after the
	// executor, since we still guarantee that the timeout is at least the configured value.
	result.StartTime = time.Now()

	go func() {
		err := executor.Stream(remotecommand.StreamOptions{
			Stdout: stdoutBuf,
			Stderr: stderrBuf,
		})
		execErrChan <- err
	}()

	// Two different cases: (1) if the command returns before the timeout, and (2) if the timeout
	// triggers before the command is done.
	// The value result.EndTime is set in both cases.
	// If the timeout triggers, then the command isn't actually cancelled. This logic isn't available until
	// client-go version 0.26 (see Executor.StreamWithContext).
	select {
	case err = <-execErrChan:
		result.EndTime = time.Now()

		if err == nil {
			break
		}

		switch e := err.(type) {
		case exec.CodeExitError:
			result.ExitCode = e.ExitStatus()

			p.statsLock.Lock()
			p.stats.NonZeroRCs++
			p.statsLock.Unlock()

			klog.V(2).Infof(
				"%s: warning: non-zero exit code %d for named command %s in pod %s/%s",
				podPeriodicCommandMeasurementName, result.ExitCode, params.Name, pod.Namespace, pod.Name,
			)
		default:
			result.ExecError = err.Error()
			return result
		}
	case <-time.After(params.Timeout):
		result.EndTime = time.Now()
		result.HitTimeout = true

		p.statsLock.Lock()
		p.stats.Timeouts++
		p.statsLock.Unlock()

		klog.V(2).Infof(
			"%s: warning: hit timeout of %s for named command %s in pod %s/%s",
			podPeriodicCommandMeasurementName, params.Timeout.String(), params.Name, pod.Namespace, pod.Name,
		)
	}

	klog.V(4).Infof(
		"%s: finished running named command %s in pod %s/%s",
		podPeriodicCommandMeasurementName, params.Name, pod.Namespace, pod.Name,
	)

	result.stdout = stdoutBuf.String()
	result.stderr = stderrBuf.String()

	return result
}

// runAllCommandsInPod runs all of the configured commands in the given specific pod.
func (p *podPeriodicCommandMeasurement) runAllCommandsInPod(pod *v1.Pod) *runAllCommandsResult {
	wg := &sync.WaitGroup{}
	commandResultCh := make(chan *runCommandResult, len(p.params.Commands))

	getRunCommandFunc := func(c *podPeriodicCommandMeasurementCommandParams) func() {
		return func() {
			defer wg.Done()

			if c := p.runCommandInPod(pod, c); c != nil {
				if c.ExecError != "" {
					p.statsLock.Lock()
					p.stats.ExecErrors++
					p.statsLock.Unlock()

					klog.V(2).Infof(
						"%s: error while running named command %s on pod %s/%s: %v",
						podPeriodicCommandMeasurementName, c.Name, pod.Namespace, pod.Name, c.ExecError,
					)
				}

				commandResultCh <- c
			}
		}
	}

	klog.V(4).Infof(
		"%s: running commands on pod %s/%s", podPeriodicCommandMeasurementName, pod.Namespace, pod.Name,
	)

	for _, command := range p.params.Commands {
		wg.Add(1)

		go getRunCommandFunc(command)()
	}

	wg.Wait()
	close(commandResultCh)

	klog.V(4).Infof(
		"%s: finished running commands on pod %s/%s", podPeriodicCommandMeasurementName, pod.Namespace, pod.Name,
	)

	results := &runAllCommandsResult{
		Pod:       pod.Name,
		Namespace: pod.Namespace,
		Container: p.params.Container,
		Commands:  []*runCommandResult{},
	}

	for c := range commandResultCh {
		results.Commands = append(results.Commands, c)
	}

	klog.V(8).Infof("%s: %#v", podPeriodicCommandMeasurementName, results)

	return results
}

// commandWorker runs the configured commands in applicable pods on the configured interval.
func (p *podPeriodicCommandMeasurement) commandWorker() {
	ticker := time.NewTicker(p.params.Interval)
	defer func() {
		ticker.Stop()
		// Close doneCh to signal the worker has exited.
		close(p.doneCh)
	}()

	doTick := func() {
		p.statsLock.Lock()
		p.stats.Ticks++
		p.statsLock.Unlock()

		targetPods := p.getMaxNPods(p.params.Limit)
		if len(targetPods) == 0 {
			klog.V(2).Infof("%s: warning: no pods available to run commands on", podPeriodicCommandMeasurementName)

			p.statsLock.Lock()
			p.stats.TicksNoPods++
			p.statsLock.Unlock()

			return
		}

		wg := &sync.WaitGroup{}
		resultsChan := make(chan *runAllCommandsResult, len(targetPods))

		for _, pod := range targetPods {
			wg.Add(1)
			go func(targetPod *v1.Pod) {
				defer wg.Done()
				resultsChan <- p.runAllCommandsInPod(targetPod)
			}(pod)
		}

		wg.Wait()
		close(resultsChan)

		for r := range resultsChan {
			p.results = append(p.results, r)
		}
	}

	// Do an initial tick
	doTick()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			doTick()
		}
	}
}

func (p *podPeriodicCommandMeasurement) start(
	clientset kubernetes.Interface, restConfig *rest.Config, params *podPeriodicCommandMeasurementParams,
) error {
	if p.isRunning {
		return fmt.Errorf("%s: measurement already running", podPeriodicCommandMeasurementName)
	}

	klog.V(2).Infof("%s: starting pod periodic command measurement...", podPeriodicCommandMeasurementName)

	p.clientset = clientset
	p.restConfig = restConfig
	p.params = params
	p.isRunning = true
	p.skipGather = false
	p.stopCh = make(chan struct{})
	p.doneCh = make(chan struct{})
	p.results = []*runAllCommandsResult{}
	p.stats = &stats{}
	p.statsLock = &sync.Mutex{}

	labelSelectorString := (*params.LabelSelector).String()
	p.informer = informer.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				options.LabelSelector = labelSelectorString
				return clientset.CoreV1().Pods("").List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = labelSelectorString
				return clientset.CoreV1().Pods("").Watch(context.TODO(), options)
			},
		},
		// Use the informer's internal cache to handle listing pods, no need to handle events.
		func(_, _ interface{}) {},
	)

	if err := informer.StartAndSync(p.informer, p.stopCh, informerSyncTimeout); err != nil {
		return err
	}

	go p.commandWorker()

	return nil
}

func (p *podPeriodicCommandMeasurement) stop() {
	if p.isRunning {
		p.isRunning = false
		close(p.stopCh)
		// Wait for the commandWorker function to stop.
		<-p.doneCh
	}
}

func (p *podPeriodicCommandMeasurement) gather() ([]measurement.Summary, error) {
	p.stop()

	klog.V(2).Infof("%s: gathered %d command results", podPeriodicCommandMeasurementName, len(p.results))

	// Create summary for all results.
	content, err := util.PrettyPrintJSON(p.results)
	if err != nil {
		// Ignore p.params.FailOnError here, since this is fatal.
		return nil, fmt.Errorf("unable to convert results to JSON: %w", err)
	}

	measurements := []measurement.Summary{
		measurement.CreateSummary(podPeriodicCommandMeasurementName, "json", content),
	}

	// Hold error to be returned to signal that the measurement failed, or nil.
	// Should only be non-nil if one of the FailOnXYZ params is set.
	var resultErr error

	// Create individual results for stdout and stderr.
	// Saving these as a value in a json document can lead to weird issues in reading the data
	// properly, especially if the data is binary, such as for profiling results.
	// Additionally, check for any errors or timeouts that may have occurred.
	for _, r := range p.results {
		getSummaryName := func(c *runCommandResult, suffix string) string {
			return strings.Join(
				[]string{
					podPeriodicCommandMeasurementName, c.StartTime.Format(time.RFC3339), r.Namespace, r.Pod, c.Name, suffix,
				}, "-",
			)
		}

		for _, c := range r.Commands {
			if c.stdout != "" {
				measurements = append(measurements, measurement.CreateSummary(getSummaryName(c, "stdout"), "txt", c.stdout))
			}
			if c.stderr != "" {
				measurements = append(measurements, measurement.CreateSummary(getSummaryName(c, "stderr"), "txt", c.stderr))
			}

			// If the result error has already been set, we don't need to set it again.
			if resultErr != nil {
				continue
			}

			if p.params.FailOnCommandError && c.ExitCode != 0 {
				resultErr = fmt.Errorf(
					"unexpected non-zero RC while executing command %s in pod %s/%s: got RC %d",
					c.Name, r.Namespace, r.Pod, c.ExitCode,
				)
				continue
			}

			if p.params.FailOnExecError && c.ExecError != "" {
				resultErr = fmt.Errorf(
					"unexpected error while executing command %s in pod %s/%s: %s", c.Name, r.Namespace, r.Pod, c.ExecError,
				)
				continue
			}

			if p.params.FailOnTimeout && c.HitTimeout {
				resultErr = fmt.Errorf(
					"hit timeout of %s while executing command %s in pod %s/%s",
					c.Timeout, c.Name, r.Namespace, r.Pod,
				)
			}
		}
	}

	// Create summary for stats.
	p.stats.Measurements = len(measurements) + 1 // Adding another measurement for the stats.
	content, err = util.PrettyPrintJSON(p.stats)
	if err != nil {
		// Ignore p.params.FailOnError here, since this is fatal.
		return nil, fmt.Errorf("unable to convert stats to JSON: %w", err)
	}

	measurements = append(
		measurements,
		measurement.CreateSummary(
			strings.Join([]string{podPeriodicCommandMeasurementName, "stats"}, "-"), "json", content,
		),
	)

	// resultErr can only be set if one of the FailOnXYZ params is set.
	if resultErr != nil {
		return measurements, resultErr
	}

	return measurements, nil
}

func (*podPeriodicCommandMeasurement) String() string {
	return podPeriodicCommandMeasurementName
}

func (p *podPeriodicCommandMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}

	switch action {
	case "start":
		params, err := newPodPeriodCommandMeasurementParams(config.Params)
		if err != nil {
			p.skipGather = true
			return nil, err
		}

		return nil, p.start(
			config.ClusterFramework.GetClientSets().GetClient(), config.ClusterFramework.GetRestClient(), params,
		)
	case "gather":
		if p.skipGather {
			return nil, nil
		}

		return p.gather()
	default:
		return nil, fmt.Errorf("unknown action %s", action)
	}
}

func (p *podPeriodicCommandMeasurement) Dispose() {
	p.stop()
}

func createPodPeriodicCommandMeasurement() measurement.Measurement {
	return &podPeriodicCommandMeasurement{}
}

func init() {
	if err := measurement.Register(podPeriodicCommandMeasurementName, createPodPeriodicCommandMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", podPeriodicCommandMeasurementName, err)
	}
}

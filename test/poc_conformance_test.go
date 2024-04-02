//go:build conformance
// +build conformance

/*
This serves as a POC for conformance test suite design including functionality,
behavioural and fields population.
It mocks the vendor service execution of TaskRuns and PipelineRuns utilizing the
Tekton clients to mock the controller of a conformant vendor service.

Please use the following for triggering the test:
go test -v -tags=conformance -count=1 ./test -run ^Test

The next step will be to integrate this test as POC with v2 API.
*/

package test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"github.com/tektoncd/pipeline/test/parse"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	knativetest "knative.dev/pkg/test"
	"knative.dev/pkg/test/helpers"
	"sigs.k8s.io/yaml"
)

const (
	TaskRunInputType       = "TaskRun"
	PipelineRunInputType   = "PipelineRun"
	ExpectRunToFail        = true
	SucceedConditionStatus = "True"
	FailureConditionStatus = "False"
)

// TODO: i.   include the dependencies in docStrings i.e.
//       ii.  separate input YAMLS in different files
//       iii. add Succeeded check for all status
//       iv.  extract generic functions to helpers i.e. checkConditionSucceeded

// TestTaskResult examines the TaskResult functionality
// by creating a TaskRun that performs multiplication in Steps to write to the
// Task-level result for validation.
func TestTaskResult(t *testing.T) {
	var multiplicand, multipliper = 3, 5

	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: TaskRun
metadata:
  name: %s
spec:
  taskSpec:
    params:
    - name: multiplicand
      description: the first operand
      default: %s
    - name: multipliper
      description: the second operand
      default: %s
    results:
    - name: product
      description: the product of the first and second operand
    steps:
    - name: add
      image: alpine
      env:
      - name: OP1
        value: $(params.multiplicand)
      - name: OP2
        value: $(params.multipliper)
      command: ["/bin/sh", "-c"]
      args:
      - echo -n $((${OP1}*${OP2})) | tee $(results.product.path);
    - name: evaluate-task-result
      image: alpine
      script: |
        if [[ $(results.product) != %s ]]; then
          exit 1
        fi
`, helpers.ObjectNameForTest(t), strconv.Itoa(multiplicand), strconv.Itoa(multipliper), strconv.Itoa(multiplicand*multipliper))

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, TaskRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedTR := parse.MustParseV1TaskRun(t, outputYAML)

	// Examining TaskRunResult
	if len(resolvedTR.Status.Results) != 1 {
		t.Errorf("Expect vendor service to provide 1 result but not")
	}

	if resolvedTR.Status.Results[0].Value.StringVal != strconv.Itoa(multiplicand*multipliper) {
		t.Errorf("Not producing correct result :%s", resolvedTR.Status.Results[0].Value.StringVal)
	}
}

// TestTaskRef tests the functionality of referencing
// to a Task that is either local or remote. The TaskRef field is REUIQRED while
// all of its fields are RECOMMENDED.
// Vendors could overwrite the actual use case of the input TaskRef.
func TestTaskRef(t *testing.T) {
	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: TaskRun
metadata:
  name: %s
spec:
  workspaces:
  - name: output
    emptyDir: {}
  params:
  - name: url
    value: https://github.com/kelseyhightower/nocode
  - name: revision
    value: master
  taskRef:
    # Vendors are welcomed to overwrite the previous section as the actual use
    # cases for TaskRef are not REQUIRED - it could be either a remote Task
    # resolved by the resolver or a local Task.
    # Below is an example used for remote Task
    resolver: git
    params:
      - name: url
        value: https://github.com/tektoncd/catalog.git
      - name: revision
        value: main
      - name: pathInRepo
        value: task/git-clone/0.8/git-clone.yaml
`, helpers.ObjectNameForTest(t))

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, TaskRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedTR := parse.MustParseV1TaskRun(t, outputYAML)

	if err := checkTaskRunConditionSucceeded(resolvedTR.Status, SucceedConditionStatus, "Succeeded"); err != nil {
		t.Error(err)
	}
}

func TestStepScript(t *testing.T) {
	expectedSteps := map[string]string{
		"noshebang":                 "Completed",
		"node":                      "Completed",
		"python":                    "Completed",
		"perl":                      "Completed",
		"params-applied":            "Completed",
		"args-allowed":              "Completed",
		"dollar-signs-allowed":      "Completed",
		"bash-variable-evaluations": "Completed",
	}

	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: TaskRun
metadata:
  name: %s
spec:
  taskSpec:
    params:
    - name: PARAM
      default: param-value
    steps:
    - name: noshebang
      image: ubuntu
      script: echo "no shebang"
    - name: node
      image: node
      script: |
        #!/usr/bin/env node
        console.log("Hello from Node!")
    - name: python
      image: python
      script: |
        #!/usr/bin/env python3
        print("Hello from Python!")
    - name: perl
      image: perl:devel-bullseye
      script: |
        #!/usr/bin/perl
        print "Hello from Perl!"
    # Test that param values are replaced.
    - name: params-applied
      image: python
      script: |
        #!/usr/bin/env python3
        v = '$(params.PARAM)'
        if v != 'param-value':
          print('Param values not applied')
          print('Got: ', v)
          exit(1)
    # Test that args are allowed and passed to the script as expected.
    - name: args-allowed
      image: ubuntu
      args: ['hello', 'world']
      script: |
        #!/usr/bin/env bash
        [[ $# == 2 ]]
        [[ $1 == "hello" ]]
        [[ $2 == "world" ]]
    # Test that multiple dollar signs next to each other are not replaced by Kubernetes
    - name: dollar-signs-allowed
      image: python
      script: |
        #!/usr/bin/env python3
        if '$' != '\u0024':
          print('single dollar signs ($) are not passed through as expected :(')
          exit(1)
        if '$$' != '\u0024\u0024':
          print('double dollar signs ($$) are not passed through as expected :(')
          exit(2)
        if '$$$' != '\u0024\u0024\u0024':
          print('three dollar signs ($$$) are not passed through as expected :(')
          exit(3)
        if '$$$$' != '\u0024\u0024\u0024\u0024':
          print('four dollar signs ($$$$) are not passed through as expected :(')
          exit(4)
        print('dollar signs appear to be handled correctly! :)')

    # Test that bash scripts with variable evaluations work as expected
    - name: bash-variable-evaluations
      image: bash:5.1.8
      script: |
        #!/usr/bin/env bash
        set -xe
        var1=var1_value
        var2=var1
        echo $(eval echo \$$var2) > tmpfile
        eval_result=$(cat tmpfile)
        if [ "$eval_result" != "var1_value" ] ; then
          echo "unexpected eval result: $eval_result"
          exit 1
        fi
`, helpers.ObjectNameForTest(t))

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, TaskRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedTR := parse.MustParseV1TaskRun(t, outputYAML)

	if len(resolvedTR.Status.Steps) != len(expectedSteps) {
		t.Errorf("Expected length of steps %v but has: %v", len(expectedSteps), len(resolvedTR.Status.Steps))
	}

	for _, resolvedStep := range resolvedTR.Status.Steps {
		resolvedStepTerminatedReason := resolvedStep.Terminated.Reason
		if expectedStepState, ok := expectedSteps[resolvedStep.Name]; ok {
			if resolvedStepTerminatedReason != expectedStepState {
				t.Fatalf("Expect step %s to have completed successfully but it has Termination Reason: %s", resolvedStep.Name, resolvedStepTerminatedReason)
			}
		} else {
			t.Fatalf("Does not expect to have step: %s", resolvedStep.Name)
		}
	}
}

func TestStepEnv(t *testing.T) {
	envVarName := "FOO"
	envVarVal := "foooooooo"

	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: TaskRun
metadata:
  name: %s
spec:
  taskSpec:
    steps:
    - name: bash
      image: ubuntu
      env:
      - name: %s
        value: %s
      script: |
        #!/usr/bin/env bash
        set -euxo pipefail
        echo "Hello from Bash!"
        echo FOO is ${FOO}
        echo substring is ${FOO:2:4}
`, helpers.ObjectNameForTest(t), envVarName, envVarVal)

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, TaskRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedTR := parse.MustParseV1TaskRun(t, outputYAML)

	resolvedStep := resolvedTR.Status.Steps[0]
	resolvedStepTerminatedReason := resolvedStep.Terminated.Reason
	if resolvedStepTerminatedReason != "Completed" {
		t.Fatalf("Expect step %s to have completed successfully but it has Termination Reason: %s", resolvedStep.Name, resolvedStepTerminatedReason)
	}

	resolvedStepEnv := resolvedTR.Status.TaskSpec.Steps[0].Env[0]
	if resolvedStepEnv.Name != envVarName {
		t.Fatalf("Expect step %s to have EnvVar Name %s but it has: %s", resolvedStep.Name, envVarName, resolvedStepEnv.Name)
	}
	if resolvedStepEnv.Value != envVarVal {
		t.Fatalf("Expect step %s to have EnvVar Value %s but it has: %s", resolvedStep.Name, envVarVal, resolvedStepEnv.Value)
	}
}

func TestStepWorkingDir(t *testing.T) {
	defaultWorkingDir := "/workspace"
	overrideWorkingDir := "/a/path/too/far"

	expectedWorkingDirs := map[string]string{
		"default":  defaultWorkingDir,
		"override": overrideWorkingDir,
	}

	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: TaskRun
metadata:
  name: %s
spec:
  taskSpec:
    steps:
    - name: default
      image: ubuntu
      workingDir: %s
      script: |
        #!/usr/bin/env bash
        if [[ $PWD != /workspace ]]; then
          exit 1
        fi
    - name: override
      image: ubuntu
      workingDir: %s
      script: |
        #!/usr/bin/env bash
        if [[ $PWD != /a/path/too/far ]]; then
          exit 1
        fi
`, helpers.ObjectNameForTest(t), defaultWorkingDir, overrideWorkingDir)

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, TaskRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedTR := parse.MustParseV1TaskRun(t, outputYAML)

	for _, resolvedStep := range resolvedTR.Status.Steps {
		resolvedStepTerminatedReason := resolvedStep.Terminated.Reason
		if resolvedStepTerminatedReason != "Completed" {
			t.Fatalf("Expect step %s to have completed successfully but it has Termination Reason: %s", resolvedStep.Name, resolvedStepTerminatedReason)
		}
	}

	for _, resolvedStepSpec := range resolvedTR.Status.TaskSpec.Steps {
		resolvedStepWorkingDir := resolvedStepSpec.WorkingDir
		if resolvedStepWorkingDir != expectedWorkingDirs[resolvedStepSpec.Name] {
			t.Fatalf("Expect step %s to have WorkingDir %s but it has: %s", resolvedStepSpec.Name, expectedWorkingDirs[resolvedStepSpec.Name], resolvedStepWorkingDir)
		}
	}
}

func TestStepStateImageID(t *testing.T) {
	// Step images can be specified by digest.
	image := "busybox@sha256:1303dbf110c57f3edf68d9f5a16c082ec06c4cf7604831669faf2c712260b5a0"
	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: TaskRun
metadata:
  name: %s
spec:
  taskSpec:
    steps:
    - image: %s
      args: ['-c', 'echo hello']
`, helpers.ObjectNameForTest(t), image)

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, TaskRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedTR := parse.MustParseV1TaskRun(t, outputYAML)

	if err := checkTaskRunConditionSucceeded(resolvedTR.Status, SucceedConditionStatus, "Succeeded"); err != nil {
		t.Error(err)
	}

	if len(resolvedTR.Status.Steps) != 1 {
		t.Errorf("Expect vendor service to provide 1 Step in StepState but it has: %v", len(resolvedTR.Status.Steps))
	}

	if !strings.HasSuffix(resolvedTR.Status.Steps[0].ImageID, image) {
		t.Errorf("Expect vendor service to provide image %s in StepState but it has: %s", image, resolvedTR.Status.Steps[0].ImageID)
	}
}

func TestStepStateName(t *testing.T) {
	stepName := "step-foo"
	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: TaskRun
metadata:
  name: %s
spec:
  taskSpec:
    steps:
    - name: %s
      image: busybox
      args: ['-c', 'echo hello']
`, helpers.ObjectNameForTest(t), stepName)

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, TaskRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedTR := parse.MustParseV1TaskRun(t, outputYAML)

	if err := checkTaskRunConditionSucceeded(resolvedTR.Status, SucceedConditionStatus, "Succeeded"); err != nil {
		t.Error(err)
	}

	if len(resolvedTR.Status.Steps) != 1 {
		t.Errorf("Expect vendor service to provide 1 Step in StepState but it has: %v", len(resolvedTR.Status.Steps))
	}

	if resolvedTR.Status.Steps[0].Name != stepName {
		t.Errorf("Expect vendor service to provide Name %s in StepState but it has: %s", stepName, resolvedTR.Status.Steps[0].Name)
	}
}

// Examines the ContainerStateTerminated ExitCode, StartedAt, FinishtedAt and Reason
func TestStepStateContainerStateTerminated(t *testing.T) {
	successInputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: TaskRun
metadata:
  name: %s
spec:
  taskSpec:
    steps:
    - image: busybox
      args: ['-c', 'echo hello']
`, helpers.ObjectNameForTest(t))

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	successOutputYAML, err := ProcessAndSendToTekton(successInputYAML, TaskRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	successResolvedTR := parse.MustParseV1TaskRun(t, successOutputYAML)

	if err := checkTaskRunConditionSucceeded(successResolvedTR.Status, SucceedConditionStatus, "Succeeded"); err != nil {
		t.Error(err)
	}

	if len(successResolvedTR.Status.Steps) != 1 {
		t.Errorf("Expect vendor service to provide 1 Step in StepState but it has: %v", len(successResolvedTR.Status.Steps))
	}

	startTime := successResolvedTR.Status.Steps[0].Terminated.StartedAt
	finishTime := successResolvedTR.Status.Steps[0].Terminated.FinishedAt

	if startTime.IsZero() {
		t.Errorf("Expect vendor service to provide StartTimeStamp in StepState.Terminated but it does not provide so")
	}

	if finishTime.IsZero() {
		t.Errorf("Expect vendor service to provide FinishTimeStamp in StepState.Terminated but it does not provide so")
	}

	if finishTime.Before(&startTime) {
		t.Errorf("Expect vendor service to provide StartTimeStamp %v earlier than FinishTimeStamp in StepState.Terminated %v but it does not provide so", startTime, finishTime)
	}

	if successResolvedTR.Status.Steps[0].Terminated.ExitCode != 0 {
		t.Errorf("Expect vendor service to provide ExitCode in StepState.Terminated to be 0 but it has: %v", successResolvedTR.Status.Steps[0].Terminated.ExitCode)
	}

	if successResolvedTR.Status.Steps[0].Terminated.Reason != "Completed" {
		t.Errorf("Expect vendor service to provide Reason in StepState.Terminated to be Completed but it has: %s", successResolvedTR.Status.Steps[0].Terminated.Reason)
	}

	failureInputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: TaskRun
metadata:
  name: %s
spec:
  taskSpec:
    steps:
    - image: busybox
      script: exit 1
`, helpers.ObjectNameForTest(t))

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	failureOutputYAML, err := ProcessAndSendToTekton(failureInputYAML, TaskRunInputType, t, ExpectRunToFail)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	failureResolvedTR := parse.MustParseV1TaskRun(t, failureOutputYAML)

	if err := checkTaskRunConditionSucceeded(failureResolvedTR.Status, FailureConditionStatus, "Failed"); err != nil {
		t.Error(err)
	}

	if len(failureResolvedTR.Status.Steps) != 1 {
		t.Errorf("Expect vendor service to provide 1 Step in StepState but it has: %v", len(failureResolvedTR.Status.Steps))
	}

	startTime = failureResolvedTR.Status.Steps[0].Terminated.StartedAt
	finishTime = failureResolvedTR.Status.Steps[0].Terminated.FinishedAt

	if startTime.IsZero() {
		t.Errorf("Expect vendor service to provide StartTimeStamp in StepState.Terminated but it does not provide so")
	}

	if finishTime.IsZero() {
		t.Errorf("Expect vendor service to provide FinishTimeStamp in StepState.Terminated but it does not provide so")
	}

	if finishTime.Before(&startTime) {
		t.Errorf("Expect vendor service to provide StartTimeStamp %v earlier than FinishTimeStamp in StepState.Terminated %v but it does not provide so", startTime, finishTime)
	}

	if failureResolvedTR.Status.Steps[0].Terminated.ExitCode != 1 {
		t.Errorf("Expect vendor service to provide ExitCode in StepState.Terminated to be 0 but it has: %v", failureResolvedTR.Status.Steps[0].Terminated.ExitCode)
	}

	if failureResolvedTR.Status.Steps[0].Terminated.Reason != "Error" {
		t.Errorf("Expect vendor service to provide Reason in StepState.Terminated to be Error but it has: %s", failureResolvedTR.Status.Steps[0].Terminated.Reason)
	}
}

func TestSidecarName(t *testing.T) {
	sidecarName := "hello-sidecar"
	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: TaskRun
metadata:
  name: %s
spec:
  taskSpec:
    sidecars:
    - name: %s
      image: ubuntu
      script: echo "hello from sidecar"
    steps:
    - name: hello-step
      image: ubuntu
      script: echo "hello from step"
`, helpers.ObjectNameForTest(t), sidecarName)

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, TaskRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedTR := parse.MustParseV1TaskRun(t, outputYAML)

	if err := checkTaskRunConditionSucceeded(resolvedTR.Status, SucceedConditionStatus, "Succeeded"); err != nil {
		t.Error(err)
	}

	if len(resolvedTR.Spec.TaskSpec.Sidecars) != 1 {
		t.Errorf("Expect vendor service to provide 1 Sidcar but it has: %v", len(resolvedTR.Spec.TaskSpec.Sidecars))
	}

	if resolvedTR.Spec.TaskSpec.Sidecars[0].Name != sidecarName {
		t.Errorf("Expect vendor service to provide Sidcar name %s but it has: %s", sidecarName, resolvedTR.Spec.TaskSpec.Sidecars[0].Name)
	}
}

// This test relies on the support of Sidecar Script and its volumeMounts.
// For sidecar tests, sidecars don't have /workspace mounted by default, so we have to define
// our own shared volume. For vendor services, please feel free to override the shared workspace
// supported in your sidecar. Otherwise there are no existing v1 conformance `REQUIRED` fields that
// are going to be used for verifying Sidecar functionality.
func TestSidecarScriptSuccess(t *testing.T) {
	succeedInputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: TaskRun
metadata:
  name: %s
spec:
  taskSpec:
    sidecars:
    - name: slow-sidecar
      image: ubuntu
      script: |
        echo "hello from sidecar" > /shared/message
      volumeMounts:
      - name: shared
        mountPath: /shared

    steps:
    - name: check-ready
      image: ubuntu
      script: cat /shared/message
      volumeMounts:
      - name: shared
        mountPath: /shared

    # Sidecars don't have /workspace mounted by default, so we have to define
    # our own shared volume.
    volumes:
    - name: shared
      emptyDir: {}
`, helpers.ObjectNameForTest(t))

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	succeedOutputYAML, err := ProcessAndSendToTekton(succeedInputYAML, TaskRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	succeededResolvedTR := parse.MustParseV1TaskRun(t, succeedOutputYAML)

	if err := checkTaskRunConditionSucceeded(succeededResolvedTR.Status, SucceedConditionStatus, "Succeeded"); err != nil {
		t.Error(err)
	}
}

func TestSidecarScriptFailure(t *testing.T) {
	failInputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: TaskRun
metadata:
  name: %s
spec:
  taskSpec:
    sidecars:
    - name: exit-sidecar
      image: ubuntu
      script: exit 1

    steps:
    - name: check-ready
      image: ubuntu
      script: cat /shared/message
      volumeMounts:
        - name: shared
          mountPath: /shared

    # Sidecars don't have /workspace mounted by default, so we have to define
    # our own shared volume.
    volumes:
    - name: shared
      emptyDir: {}
`, helpers.ObjectNameForTest(t))

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	failOutputYAML, err := ProcessAndSendToTekton(failInputYAML, TaskRunInputType, t, ExpectRunToFail)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	failResolvedTR := parse.MustParseV1TaskRun(t, failOutputYAML)

	if len(failResolvedTR.Spec.TaskSpec.Sidecars) != 1 {
		t.Errorf("Expect vendor service to provide 1 Sidcar but it has: %v", len(failResolvedTR.Spec.TaskSpec.Sidecars))
	}

	if err := checkTaskRunConditionSucceeded(failResolvedTR.Status, "False", "Failed"); err != nil {
		t.Error(err)
	}
}

func TestSidecarArgAndCommand(t *testing.T) {
	failInputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: TaskRun
metadata:
  name: %s
spec:
  taskSpec:
    sidecars:
    - name: slow-sidecar
      image: ubuntu
      command: [/bin/bash]
      args: [-c, "echo 'hello from sidecar' > /shared/message"]
      volumeMounts:
      - name: shared
        mountPath: /shared
    steps:
    - name: check-ready
      image: ubuntu
      command:
      - cat
      args:
      - '/shared/message'
      volumeMounts:
      - name: shared
        mountPath: /shared
    
    # Sidecars don't have /workspace mounted by default, so we have to define
    # our own shared volume.
    volumes:
    - name: shared
      emptyDir: {}
`, helpers.ObjectNameForTest(t))

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	failOutputYAML, err := ProcessAndSendToTekton(failInputYAML, TaskRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	failResolvedTR := parse.MustParseV1TaskRun(t, failOutputYAML)

	if len(failResolvedTR.Spec.TaskSpec.Sidecars) != 1 {
		t.Errorf("Expect vendor service to provide 1 Sidcar but it has: %v", len(failResolvedTR.Spec.TaskSpec.Sidecars))
	}

	if err := checkTaskRunConditionSucceeded(failResolvedTR.Status, SucceedConditionStatus, "Succeeded"); err != nil {
		t.Error(err)
	}
}

func TestStringTaskParam(t *testing.T) {
	stringParam := "foo-string"

	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: TaskRun
metadata:
  name: %s
spec:
  params:
    - name: "string-param"
      value: %s
  taskSpec:
    params:
      - name: "string-param"
        type: string
    steps:
      - name: "check-param"
        image: bash
        script: |
          if [[ $(params.string-param) != %s ]]; then
            exit 1
          fi
`, helpers.ObjectNameForTest(t), stringParam, stringParam)

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, TaskRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedTR := parse.MustParseV1TaskRun(t, outputYAML)

	if len(resolvedTR.Spec.Params) != 1 {
		t.Errorf("Expect vendor service to provide 1 Param but it has: %v", len(resolvedTR.Spec.Params))
	}

	if err := checkTaskRunConditionSucceeded(resolvedTR.Status, SucceedConditionStatus, "Succeeded"); err != nil {
		t.Error(err)
	}

}

func TestArrayTaskParam(t *testing.T) {
	var arrayParam0, arrayParam1 = "foo", "bar"

	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: TaskRun
metadata:
  name: %s
spec:
  params:
    - name: array-to-concat
      value:
        - %s
        - %s
  taskSpec:
    results:
    - name: "concat-array"
    params:
      - name: array-to-concat
        type: array
    steps:
    - name: concat-array-params
      image: alpine
      command: ["/bin/sh", "-c"]
      args:
      - echo -n $(params.array-to-concat[0])"-"$(params.array-to-concat[1]) | tee $(results.concat-array.path);
`, helpers.ObjectNameForTest(t), arrayParam0, arrayParam1)

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, TaskRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedTR := parse.MustParseV1TaskRun(t, outputYAML)

	if len(resolvedTR.Spec.Params) != 1 {
		t.Errorf("Examining TaskRun Param: expect vendor service to provide TaskRun with 1 Array Param but it has: %v", len(resolvedTR.Spec.Params))
	}
	if len(resolvedTR.Spec.Params[0].Value.ArrayVal) != 2 {
		t.Errorf("Examining TaskParams: expect vendor service to provide 2 Task Array Param values but it has: %v", len(resolvedTR.Spec.Params[0].Value.ArrayVal))
	}

	// Utilizing TaskResult to verify functionality of Array Params
	if len(resolvedTR.Status.Results) != 1 {
		t.Errorf("Expect vendor service to provide 1 result but it has: %v", len(resolvedTR.Status.Results))
	}
	if resolvedTR.Status.Results[0].Value.StringVal != arrayParam0+"-"+arrayParam1 {
		t.Errorf("Not producing correct result, expect to get \"%s\" but has: \"%s\"", arrayParam0+"-"+arrayParam1, resolvedTR.Status.Results[0].Value.StringVal)
	}
}

func TestTaskParamDefaults(t *testing.T) {
	stringParam := "string-foo"
	arrayParam := []string{"array-foo", "array-bar"}
	expectedStringParamResultVal := "string-foo-string-baz-default"
	expectedArrayParamResultVal := "array-foo-array-bar-default"

	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: TaskRun
metadata:
  name: %s
spec:
  params:
    - name: array-param
      value:
        - %s
        - %s
    - name: string-param
      value: %s
  taskSpec:
    results:
    - name: array-output
    - name: string-output
    params:
      - name: array-param
        type: array
      - name: array-defaul-param
        type: array
        default:
        - "array-foo-default"
        - "array-bar-default"
      - name: string-param
        type: string
      - name: string-default
        type: string
        default: "string-baz-default"
    steps:
      - name: string-params-to-result
        image: bash:3.2
        command: ["/bin/sh", "-c"]
        args:
        - echo -n $(params.string-param)"-"$(params.string-default) | tee $(results.string-output.path);
      - name: array-params-to-result
        image: bash:3.2
        command: ["/bin/sh", "-c"]
        args:
        - echo -n $(params.array-param[0])"-"$(params.array-defaul-param[1]) | tee $(results.array-output.path);
`, helpers.ObjectNameForTest(t), arrayParam[0], arrayParam[1], stringParam)

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, TaskRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedTR := parse.MustParseV1TaskRun(t, outputYAML)

	if len(resolvedTR.Spec.Params) != 2 {
		t.Errorf("Expect vendor service to provide 2 Params but it has: %v", len(resolvedTR.Spec.Params))
	}
	if len(resolvedTR.Spec.Params[0].Value.ArrayVal) != 2 {
		t.Errorf("Expect vendor service to provide 2 Task Array Params but it has: %v", len(resolvedTR.Spec.Params))
	}
	for _, param := range resolvedTR.Spec.Params {
		if param.Name == "array-param" {
			paramArr := param.Value.ArrayVal
			for i, _ := range paramArr {
				if paramArr[i] != arrayParam[i] {
					t.Errorf("Expect Params to match %s: %v", arrayParam[i], paramArr[i])
				}
			}
		}
		if param.Name == "string-param" {
			if param.Value.StringVal != stringParam {
				t.Errorf("Not producing correct result, expect to get \"%s\" but has: \"%s\"", stringParam, param.Value.StringVal)
			}
		}
	}

	// Utilizing TaskResult to verify functionality of Task Params Defaults
	if len(resolvedTR.Status.Results) != 2 {
		t.Errorf("Expect vendor service to provide 2 result but it has: %v", len(resolvedTR.Status.Results))
	}

	for _, result := range resolvedTR.Status.Results {
		if result.Name == "string-output" {
			resultVal := result.Value.StringVal
			if resultVal != expectedStringParamResultVal {
				t.Errorf("Not producing correct result, expect to get \"%s\" but has: \"%s\"", expectedStringParamResultVal, resultVal)
			}
		}
		if result.Name == "array-output" {
			resultVal := result.Value.StringVal
			if resultVal != expectedArrayParamResultVal {
				t.Errorf("Not producing correct result, expect to get \"%s\" but has: \"%s\"", expectedArrayParamResultVal, resultVal)
			}
		}
	}
}

func TestTaskParamDescription(t *testing.T) {
	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: TaskRun
metadata:
    name: %s
spec:
  taskSpec:
    params:
    - name: foo
      description: foo param
      default: "foo"
    steps:
    - name: add
      image: alpine
      env:
      - name: OP1
        value: $(params.foo)
      command: ["/bin/sh", "-c"]
      args:
        - echo -n ${OP1}
`, helpers.ObjectNameForTest(t))

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, TaskRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedTR := parse.MustParseV1TaskRun(t, outputYAML)

	if resolvedTR.Spec.TaskSpec.Params[0].Description != "foo param" {
		t.Errorf("Expect vendor service to provide Param Description \"foo param\" but it has: %s", resolvedTR.Spec.TaskSpec.Params[0].Description)
	}

	if resolvedTR.Status.TaskSpec.Params[0].Description != "foo param" {
		t.Errorf("Expect vendor service to provide Param Description \"foo param\" but it has: %s", resolvedTR.Spec.TaskSpec.Params[0].Description)
	}
}

// The goal of the Taskrun Workspace test is to verify if different Steps in the TaskRun could
// pass data among each other.
func TestTaskRunWorkspace(t *testing.T) {
	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: TaskRun
metadata:
  name: %s
spec:
  workspaces:
    - name: custom-workspace
      # Please note that vendor services are welcomed to override the following actual workspace binding type.
      # This is considered as the implementation detail for the conformant workspace fields.
      emptyDir: {}
  taskSpec:
    steps:
    - name: write
      image: ubuntu
      script: echo $(workspaces.custom-workspace.path) > $(workspaces.custom-workspace.path)/foo
    - name: read
      image: ubuntu
      script: cat $(workspaces.custom-workspace.path)/foo
    - name: check
      image: ubuntu
      script: |
        if [ "$(cat $(workspaces.custom-workspace.path)/foo)" != "/workspace/custom-workspace" ]; then
          echo $(cat $(workspaces.custom-workspace.path)/foo)
          exit 1
        fi
    workspaces:
    - name: custom-workspace
`, helpers.ObjectNameForTest(t))

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, TaskRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedTR := parse.MustParseV1TaskRun(t, outputYAML)

	if err := checkTaskRunConditionSucceeded(resolvedTR.Status, SucceedConditionStatus, "Succeeded"); err != nil {
		t.Error(err)
	}

	if len(resolvedTR.Spec.Workspaces) != 1 {
		t.Errorf("Expect vendor service to provide 1 Workspace but it has: %v", len(resolvedTR.Spec.Workspaces))
	}

	if resolvedTR.Spec.Workspaces[0].Name != "custom-workspace" {
		t.Errorf("Expect vendor service to provide Workspace 'custom-workspace' but it has: %s", resolvedTR.Spec.Workspaces[0].Name)
	}

	if resolvedTR.Status.TaskSpec.Workspaces[0].Name != "custom-workspace" {
		t.Errorf("Expect vendor service to provide Workspace 'custom-workspace' in TaskRun.Status.TaskSpec but it has: %s", resolvedTR.Spec.Workspaces[0].Name)
	}
}

// TestTaskRunTimeout examines the Timeout behaviour for
// TaskRun level. It creates a TaskRun with Timeout and wait in the Step of the
// inline Task for the time length longer than the specified Timeout.
// The TaskRun is expected to fail with the Reason `TaskRunTimeout`.
func TestTaskRunTimeout(t *testing.T) {
	expectedFailedStatus := true
	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: TaskRun
metadata:
  name: %s
spec:
  timeout: 15s
  taskSpec:
    steps:
    - image: busybox
      command: ['/bin/sh']
      args: ['-c', 'sleep 15001']
`, helpers.ObjectNameForTest(t))

	// Execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, TaskRunInputType, t, expectedFailedStatus)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedTR := parse.MustParseV1TaskRun(t, outputYAML)

	if err := checkTaskRunConditionSucceeded(resolvedTR.Status, "False", "TaskRunTimeout"); err != nil {
		t.Error(err)
	}
}

// TestConditions examines population of Conditions
// fields. It creates the a TaskRun with minimal specifications and checks the
// required Condition Status and Type.
func TestConditions(t *testing.T) {
	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: TaskRun
metadata:
  name: %s
spec:
  taskSpec:
    steps:
    - name: add
      image: ubuntu
      script:
        echo Hello world!
`, helpers.ObjectNameForTest(t))

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, TaskRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedTR := parse.MustParseV1TaskRun(t, outputYAML)

	if err := checkTaskRunConditionSucceeded(resolvedTR.Status, SucceedConditionStatus, "Succeeded"); err != nil {
		t.Error(err)
	}
}

// TestPipelineTaskParams examines the PipelineTask
// Params functionality by creating a Pipeline that performs addition in its
// Task for validation.
func TestPipelineTaskParams(t *testing.T) {
	var op0, op1 = 10, 1
	expectedParams := v1.Params{{
		Name:  "op0",
		Value: v1.ParamValue{StringVal: strconv.Itoa(op0)},
	}, {
		Name:  "op1",
		Value: v1.ParamValue{StringVal: strconv.Itoa(op1)}},
	}

	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: PipelineRun
metadata:
  name: %s
spec:
  pipelineSpec:
    tasks:
    - name: sum-params
      taskSpec:
        params:
        - name: op0
          type: string
          description: The first integer from PipelineTask Param
        - name: op1
          type: string
          description: The second integer from PipelineTask Param
        steps:
        - name: sum
          image: bash:latest
          script: |
            #!/usr/bin/env bash
            echo -n $(( "$(inputs.params.op0)" + "$(inputs.params.op1)" ))
      params:
      - name: op0
        value: %s
      - name: op1
        value: %s
`, helpers.ObjectNameForTest(t), strconv.Itoa(op0), strconv.Itoa(op1))

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, PipelineRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedPR := parse.MustParseV1PipelineRun(t, outputYAML)
	if len(resolvedPR.Spec.PipelineSpec.Tasks) != 1 {
		t.Errorf("Expect vendor service to provide 1 PipelineTask but got: %v", len(resolvedPR.Spec.PipelineSpec.Tasks))
	}

	if d := cmp.Diff(expectedParams, resolvedPR.Spec.PipelineSpec.Tasks[0].Params, cmpopts.IgnoreFields(v1.ParamValue{}, "Type")); d != "" {
		t.Errorf("Expect vendor service to provide 2 params 10, 1, but got: %v", d)

	}
}

func TestPipelineResult(t *testing.T) {
	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: PipelineRun
metadata:
  name: %s
spec:
  params:
  - name: prefix
    value: prefix
  pipelineSpec:
    results:
    - name: output
      type: string
      value: $(tasks.do-something.results.output)
    params:
    - name: prefix
    tasks:
    - name: generate-suffix
      taskSpec:
        results:
        - name: suffix
        steps:
        - name: generate-suffix
          image: alpine
          script: |
            echo -n "suffix" > $(results.suffix.path)
    - name: do-something
      taskSpec:
        results:
        - name: output
        params:
        - name: arg
        steps:
        - name: do-something
          image: alpine
          script: |
            echo -n "$(params.arg)" | tee $(results.output.path)
      params:
      - name: arg
        value: "$(params.prefix):$(tasks.generate-suffix.results.suffix)"
`, helpers.ObjectNameForTest(t))

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, PipelineRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedPR := parse.MustParseV1PipelineRun(t, outputYAML)

	if len(resolvedPR.Status.Results) != 1 {
		t.Errorf("Expect vendor service to provide 1 result but has: %v", len(resolvedPR.Status.Results))
	}

	if resolvedPR.Status.Results[0].Value.StringVal != "prefix:suffix" {
		t.Errorf("Not producing correct result :\"%s\"", resolvedPR.Status.Results[0].Value.StringVal)
	}
}

func TestPipelineWorkspace(t *testing.T) {
	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: PipelineRun
metadata:
  name: %s
spec:
  workspaces:
  - name: custom-workspace
    # Vendor service could override the actual workspace binding type.
    # This is considered as the implementation detail for the conformant workspace fields.
    volumeClaimTemplate:
      spec:
        accessModes:
          - ReadWriteOnce
        resources:
          requests:
            storage: 16Mi
        volumeMode: Filesystem
  pipelineSpec:
    workspaces:
    - name: custom-workspace
    tasks:
    - name: write-task
      taskSpec:
        steps:
        - name: write-step
          image: ubuntu
          script: |
            echo $(workspaces.custom-workspace-write-task.path) > $(workspaces.custom-workspace-write-task.path)/foo
            cat $(workspaces.custom-workspace-write-task.path)/foo
        workspaces:
        - name: custom-workspace-write-task
      workspaces:
      - name: custom-workspace-write-task
        workspace: custom-workspace
    - name: read-task
      taskSpec:
        steps:
        - name: read-step
          image: ubuntu
          script: cat $(workspaces.custom-workspace-read-task.path)/foo
        workspaces:
        - name: custom-workspace-read-task
      workspaces:
      - name: custom-workspace-read-task
        workspace: custom-workspace
      runAfter:
      - write-task
    - name: check-task
      taskSpec:
        steps:
        - name: check-step
          image: ubuntu
          script: |
            if [ "$(cat $(workspaces.custom-workspace-check-task.path)/foo)" != "/workspace/custom-workspace-write-task" ]; then
              echo $(cat $(workspaces.custom-workspace-check-task.path)/foo)
              exit 1
            fi
        workspaces:
        - name: custom-workspace-check-task
      workspaces:
      - name: custom-workspace-check-task
        workspace: custom-workspace
      runAfter:
      - read-task
`, helpers.ObjectNameForTest(t))

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, PipelineRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedPR := parse.MustParseV1PipelineRun(t, outputYAML)

	if err := checkPipelineRunConditionSucceeded(resolvedPR.Status, SucceedConditionStatus, "Succeeded"); err != nil {
		t.Error(err)
	}

	if resolvedPR.Spec.Workspaces[0].Name != "custom-workspace" {
		t.Errorf("Expect vendor service to provide Workspace 'custom-workspace' but it has: %s", resolvedPR.Spec.Workspaces[0].Name)
	}

	if resolvedPR.Status.PipelineSpec.Workspaces[0].Name != "custom-workspace" {
		t.Errorf("Expect vendor service to provide Workspace 'custom-workspace' in PipelineRun.Status.TaskSpec but it has: %s", resolvedPR.Spec.Workspaces[0].Name)
	}

	// TODO add more tests for WorkSpace Declaration test for PipelineTask Workspace in a separate test
}

func TestPipelineTaskTimeout(t *testing.T) {
	expectedFailedStatus := true
	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: PipelineRun
metadata:
  name: %s
spec:
  pipelineSpec:
    tasks:
    - name: timeout
      timeout: 15s
      taskSpec:
        steps:
        - image: busybox
          command: ['/bin/sh']
          args: ['-c', 'sleep 15001']
`, helpers.ObjectNameForTest(t))

	// Execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, PipelineRunInputType, t, expectedFailedStatus)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedPR := parse.MustParseV1PipelineRun(t, outputYAML)

	// TODO to examine PipelineRunReason when https://github.com/tektoncd/pipeline/issues/7573 is fixed - PipelineTaskTimeout
	if err := checkPipelineRunConditionSucceeded(resolvedPR.Status, FailureConditionStatus, "Failed"); err != nil {
		t.Error(err)
	}
}

// TestPipelineRunTimeout examines the Timeout behaviour for
// PipelineRun level. It creates a TaskRun with Timeout and wait in the Step of the
// inline Task for the time length longer than the specified Timeout.
// The TaskRun is expected to fail with the Reason `TaskRunTimeout`.
func TestPipelineRunTimeout(t *testing.T) {
	expectedFailedStatus := true
	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: PipelineRun
metadata:
  name: %s
spec:
  timeouts:
    tasks: 15s
  pipelineSpec:
    tasks:
    - name: timeout
      taskSpec:
        steps:
        - image: busybox
          command: ['/bin/sh']
          args: ['-c', 'sleep 15001']
`, helpers.ObjectNameForTest(t))

	// Execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, PipelineRunInputType, t, expectedFailedStatus)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedPR := parse.MustParseV1PipelineRun(t, outputYAML)

	if err := checkPipelineRunConditionSucceeded(resolvedPR.Status, FailureConditionStatus, "PipelineRunTimeout"); err != nil {
		t.Error(err)
	}

}

// ** there is no feasible way as in v1 conformance policy to test finally without
// dependencies: results, param functionality
func TestPipelineRunTaskFinally(t *testing.T) {
	var inputOp0, inputOp1 = 3, 1
	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: PipelineRun
metadata:
  name: %s
spec:
  pipelineSpec:
    params:
      - name: a
        type: string
      - name: b
        type: string
    results:
      - name: task-result
        description: "grabbing results from the tasks section"
        value: $(tasks.multiply-inputs.results.product)
      - name: finally-result
        description: "grabbing results from the finally section"
        value: $(finally.exponent.results.product)
    tasks:
      - name: multiply-inputs
        taskSpec:
          results:
            - name: product
              description: The product of the two provided integers
          steps:
            - name: product
              image: bash:latest
              script: |
                #!/usr/bin/env bash
                echo -n $(( "$(params.a)" * "$(params.b)" )) | tee $(results.product.path)
        params:
          - name: a
            value: "$(params.a)"
          - name: b
            value: "$(params.b)"
    finally:
      - name: exponent
        taskSpec:
          results:
            - name: product
              description: The product of the two provided integers
          steps:
            - name: product
              image: bash:latest
              script: |
                #!/usr/bin/env bash
                echo -n $(( "$(params.a)" * "$(params.b)" )) | tee $(results.product.path)
        params:
          - name: a
            value: "$(tasks.multiply-inputs.results.product)$(tasks.multiply-inputs.results.product)"
          - name: b
            value: "$(tasks.multiply-inputs.results.product)$(tasks.multiply-inputs.results.product)"
  params:
    - name: a
      value: %s
    - name: b
      value: %s
`, helpers.ObjectNameForTest(t), strconv.Itoa(inputOp0), strconv.Itoa(inputOp1))

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, PipelineRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedPR := parse.MustParseV1PipelineRun(t, outputYAML)
	if len(resolvedPR.Status.Conditions) != 1 {
		t.Errorf("Expect vendor service to populate 1 Condition but no")
	}

	expectedFinallyResultVal := strconv.Itoa((inputOp0*10 + inputOp0) * (inputOp1*10 + inputOp1) * inputOp0 * inputOp1)

	for _, res := range resolvedPR.Status.Results {
		if res.Name == "finally-result" {
			if res.Value.StringVal != expectedFinallyResultVal {
				t.Errorf("Expect vendor service to provide finally task computation to have resultVal %s, but has: %s", expectedFinallyResultVal, res.Value.StringVal)
			}
		}
	}
}

// TestPipelineRunConditions examines population of Conditions
// fields. It creates the a PipelineRun with minimal specifications and checks the
// required Condition Status and Type.
func TestPipelineRunConditions(t *testing.T) {
	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: PipelineRun
metadata:
  name: %s
spec:
  pipelineSpec:
    tasks:
    - name: pipeline-task-0
      taskSpec:
        steps:
        - name: add
          image: ubuntu
          script:
            echo Hello world!
`, helpers.ObjectNameForTest(t))

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, PipelineRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedPR := parse.MustParseV1PipelineRun(t, outputYAML)

	if err := checkPipelineRunConditionSucceeded(resolvedPR.Status, SucceedConditionStatus, "Succeeded"); err != nil {
		t.Error(err)
	}
}

func TestPipelineRunChildReferences(t *testing.T) {
	prName := helpers.ObjectNameForTest(t)
	pt0, pt1 := "pipeline-task-0", "pipeline-task-1"
	expectedChildRefs := map[string]string{
		pt0: prName + "-" + pt0,
		pt1: prName + "-" + pt1,
	}

	inputYAML := fmt.Sprintf(`
apiVersion: tekton.dev/v1
kind: PipelineRun
metadata:
  name: %s
spec:
  pipelineSpec:
    tasks:
    - name: %s
      taskSpec:
        steps:
        - name: hello-step
          image: ubuntu
          script:
            echo Hello world!
    - name: %s
      taskSpec:
        steps:
        - name: hell-step
          image: ubuntu
          script:
            echo Hello world!
`, prName, pt0, pt1)

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, PipelineRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedPR := parse.MustParseV1PipelineRun(t, outputYAML)

	if err := checkPipelineRunConditionSucceeded(resolvedPR.Status, SucceedConditionStatus, "Succeeded"); err != nil {
		t.Error(err)
	}

	if len(resolvedPR.Status.ChildReferences) != 2 {
		t.Errorf("Expect vendor service to have 2 ChildReferences but it has: %v", len(resolvedPR.Status.ChildReferences))
	}

	for _, cr := range resolvedPR.Status.ChildReferences {
		if childRefName, ok := expectedChildRefs[cr.PipelineTaskName]; ok {
			if childRefName != cr.Name {
				t.Errorf("Expect vendor service to populate ChildReferenceStatus Name %s but it has: %s", childRefName, cr.Name)
			}
		} else {
			t.Errorf("Does not expect vendor service to populate ChildReferenceStatus PipelineTaskName: %s", cr.PipelineTaskName)
		}
	}

}

// ProcessAndSendToTekton takes in vanilla Tekton PipelineRun and TaskRun, waits for the object to succeed and outputs the final PipelineRun and TaskRun with status.
// The parameters are inputYAML and its Primitive type {PipelineRun, TaskRun}
// And the return values will be the output YAML string and errors.
func ProcessAndSendToTekton(inputYAML, primitiveType string, customInputs ...interface{}) (string, error) {
	// Handle customInputs
	var t *testing.T
	var expectRunToFail bool
	for _, customInput := range customInputs {
		if ci, ok := customInput.(*testing.T); ok {
			t = ci
		}
		if ci, ok := customInput.(bool); ok {
			expectRunToFail = ci
		}
	}

	return mockTektonPipelineController(t, inputYAML, primitiveType, expectRunToFail)
}

// mockTektonPipelineController fakes the behaviour of a vendor service by utilizing the Tekton test infrastructure.
// For the POC, it uses the Tetkon clients to Create, Wait for and Get the expected TaskRun.
func mockTektonPipelineController(t *testing.T, inputYAML, primitiveType string, expectRunToFail bool) (string, error) {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	c, namespace := setup(ctx, t)
	knativetest.CleanupOnInterrupt(func() { tearDown(ctx, t, c, namespace) }, t.Logf)
	defer tearDown(ctx, t, c, namespace)

	mvs := MockVendorSerivce{cs: c}

	var outputYAML []byte
	switch primitiveType {
	case TaskRunInputType:
		tr, err := mvs.CreateTaskRun(ctx, inputYAML)
		if err != nil {
			return "", err
		}

		if err := mvs.WaitForTaskRun(ctx, tr.Name, expectRunToFail); err != nil {
			return "", err
		}

		trGot, err := mvs.GetTaskRun(ctx, tr.Name)
		if err != nil {
			return "", err
		}

		outputYAML, err = yaml.Marshal(trGot)
		if err != nil {
			return "", err
		}
	case PipelineRunInputType:
		pr, err := mvs.CreatePipelineRun(ctx, inputYAML)
		if err != nil {
			return "", err
		}

		if err := mvs.WaitForPipelineRun(ctx, pr.Name, expectRunToFail); err != nil {
			return "", err
		}

		prGot, err := mvs.GetPipelineRun(ctx, pr.Name)
		if err != nil {
			return "", err
		}

		outputYAML, err = yaml.Marshal(prGot)
		if err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("invalid input primitive type: %s", primitiveType)
	}

	return string(outputYAML[:]), nil
}

type VendorService interface {
	CreateTaskRun(ctx context.Context, inputYAML string) (*v1.TaskRun, error)
	WaitForTaskRun(ctx context.Context, name string, expectRunToFail bool) error
	GetTaskRun(ctx context.Context, name string) (*v1.TaskRun, error)
	CreatePipelineRun(ctx context.Context, inputYAML string) (*v1.TaskRun, error)
	WaitForPipelineRun(ctx context.Context, name string, expectRunToFail bool) error
	GetPipelineRun(ctx context.Context, name string) (*v1.TaskRun, error)
}

type MockVendorSerivce struct {
	cs *clients
}

// CreateTaskRun parses the inputYAML to a TaskRun and creates the TaskRun via TaskRunClient
func (mvs MockVendorSerivce) CreateTaskRun(ctx context.Context, inputYAML string) (*v1.TaskRun, error) {
	var tr v1.TaskRun
	if _, _, err := scheme.Codecs.UniversalDeserializer().Decode([]byte(inputYAML), nil, &tr); err != nil {
		return nil, fmt.Errorf("must parse YAML (%s): %v", inputYAML, err)
	}

	var trCreated *v1.TaskRun
	trCreated, err := mvs.cs.V1TaskRunClient.Create(ctx, &tr, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create TaskRun `%v`: %w", tr, err)
	}
	return trCreated, nil
}

// CreateTaskRun waits for the TaskRun to get done according to the expected Condition Accessor function
func (mvs MockVendorSerivce) WaitForTaskRun(ctx context.Context, name string, expectRunToFail bool) error {
	var caf ConditionAccessorFn
	caf = Succeed(name)
	if expectRunToFail {
		caf = Failed(name)
	}
	if err := WaitForTaskRunState(ctx, mvs.cs, name, caf, "WaitTaskRunDone", v1Version); err != nil {
		return fmt.Errorf("error waiting for TaskRun to finish: %s", err)
	}
	return nil
}

// CreateTaskRun retrieves the TaskRun via TaskRunClient
func (mvs MockVendorSerivce) GetTaskRun(ctx context.Context, name string) (*v1.TaskRun, error) {
	trGot, err := mvs.cs.V1TaskRunClient.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get TaskRun `%s`: %s", trGot.Name, err)
	}
	return trGot, nil
}

func (mvs MockVendorSerivce) CreatePipelineRun(ctx context.Context, inputYAML string) (*v1.PipelineRun, error) {
	var pr v1.PipelineRun
	if _, _, err := scheme.Codecs.UniversalDeserializer().Decode([]byte(inputYAML), nil, &pr); err != nil {
		return nil, fmt.Errorf("must parse YAML (%s): %v", inputYAML, err)
	}

	var prCreated *v1.PipelineRun
	prCreated, err := mvs.cs.V1PipelineRunClient.Create(ctx, &pr, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create PipelineRun `%v`: %w", pr, err)
	}
	return prCreated, nil
}

func (mvs MockVendorSerivce) WaitForPipelineRun(ctx context.Context, name string, expectRunToFail bool) error {
	var caf ConditionAccessorFn
	caf = Succeed(name)
	if expectRunToFail {
		caf = Failed(name)
	}
	if err := WaitForPipelineRunState(ctx, mvs.cs, name, timeout, caf, "WaitPipelineRunDone", v1Version); err != nil {
		return fmt.Errorf("error waiting for PipelineRun to finish: %s", err)
	}
	return nil
}

func (mvs MockVendorSerivce) GetPipelineRun(ctx context.Context, name string) (*v1.PipelineRun, error) {
	prGot, err := mvs.cs.V1PipelineRunClient.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get PipelineRun `%s`: %s", prGot.Name, err)
	}
	return prGot, nil
}

// checkTaskRunConditionSucceeded checks the TaskRun Succeeded Condition;
// expectedSucceeded is a corev1.ConditionStatus(string), which is either "True" or "False"
// expectedReason is string, the expected Condition.Reason
func checkTaskRunConditionSucceeded(trStatus v1.TaskRunStatus, expectedSucceededStatus string, expectedReason string) error {
	hasSucceededConditionType := false

	for _, cond := range trStatus.Conditions {
		if cond.Type == "Succeeded" {
			if string(cond.Status) != expectedSucceededStatus {
				return fmt.Errorf("Expect vendor service to populate Condition %s but got: %s", expectedSucceededStatus, cond.Status)
			}
			if cond.Reason != expectedReason {
				return fmt.Errorf("Expect vendor service to populate Condition Reason %s but got: %s", expectedReason, cond.Reason)
			}

			hasSucceededConditionType = true
		}
	}

	if !hasSucceededConditionType {
		return fmt.Errorf("Expect vendor service to populate Succeeded Condition but not apparent in TaskRunStatus")
	}

	return nil
}

// checkPipelineRunConditionSucceeded checks the PipelineRun Succeeded Condition;
// expectedSucceeded is a corev1.ConditionStatus(string), which is either "True" or "False"
// expectedReason is string, the expected Condition.Reason
func checkPipelineRunConditionSucceeded(prStatus v1.PipelineRunStatus, expectedSucceededStatus string, expectedReason string) error {
	hasSucceededConditionType := false

	for _, cond := range prStatus.Conditions {
		if cond.Type == "Succeeded" {
			if string(cond.Status) != expectedSucceededStatus {
				return fmt.Errorf("Expect vendor service to populate Condition %s but got: %s", expectedSucceededStatus, cond.Status)
			}
			if cond.Reason != expectedReason {
				return fmt.Errorf("Expect vendor service to populate Condition Reason %s but got: %s", expectedReason, cond.Reason)
			}

			hasSucceededConditionType = true
		}
	}

	if !hasSucceededConditionType {
		return fmt.Errorf("Expect vendor service to populate Succeeded Condition but not apparent in PipelineRunStatus")
	}

	return nil
}

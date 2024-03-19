//go:build conformance
// +build conformance

/*
This serves as a POC for conformance test suite design including functionality,
behavioural and fields population.
It mocks the vendor service execution of TaskRuns and PipelineRuns utilizing the
Tekton clients to mock the controller of a conformant vendor service.

Please use the following for triggering the test:
go test -v -tags=conformance -count=1 ./test -run ^TestConformance

The next step will be to integrate this test as POC with v2 API.
*/

package test

import (
	"context"
	"fmt"
	"strconv"
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
	TaskRunInputType     = "TaskRun"
	PipelineRunInputType = "PipelineRun"
)

// TODO: i.   include the dependencies in docStrings i.e.
//       ii.  separate input YAMLS in different files
//       iii. add Succeeded check for all status
//       iv.  extract generic functions to helpers i.e. checkConditionSucceeded

// TestConformanceShouldProvideTaskResult examines the TaskResult functionality
// by creating a TaskRun that performs multiplication in Steps to write to the
// Task-level result for validation.
func TestConformanceShouldProvideTaskResult(t *testing.T) {
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
      ti mw:
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

func TestConformanceShouldProvideStepScript(t *testing.T) {
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

func TestConformanceShouldProvideStepEnv(t *testing.T) {
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

func TestConformanceShouldProvideStepWorkingDir(t *testing.T) {
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

func TestConformanceShouldProvideStringTaskParam(t *testing.T) {
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

	hasSucceededConditionType := false

	for _, cond := range resolvedTR.Status.Conditions {
		if cond.Type == "Succeeded" {
			if cond.Status != "True" {
				t.Errorf("Expect vendor service to populate Condition `True` but got: %s", cond.Status)
			}
			if cond.Reason != "Succeeded" {
				t.Errorf("Expect vendor service to populate Condition Reason `Succeeded` but got: %s", cond.Reason)
			}
			hasSucceededConditionType = true
		}
	}

	if !hasSucceededConditionType {
		t.Errorf("Expect vendor service to populate Succeeded Condition but not apparent in TaskRunStatus")
	}

}

func TestConformanceShouldProvideArrayTaskParam(t *testing.T) {
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

func TestConformanceShouldProvideTaskParamDefaults(t *testing.T) {
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

func TestConformanceShouldProvideTaskParamDescription(t *testing.T) {
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
func TestConformanceShouldProvideTaskRunWorkspace(t *testing.T) {
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

	hasSucceededConditionType := false

	for _, cond := range resolvedTR.Status.Conditions {
		if cond.Type == "Succeeded" {
			if cond.Status != "True" {
				t.Errorf("Expect vendor service to populate Condition `True` but got: %s", cond.Status)
			}
			if cond.Reason != "Succeeded" {
				t.Errorf("Expect vendor service to populate Condition Reason `Succeeded` but got: %s", cond.Reason)
			}
			hasSucceededConditionType = true
		}
	}

	if !hasSucceededConditionType {
		t.Errorf("Expect vendor service to populate Succeeded Condition but not apparent in TaskRunStatus")
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

// TestConformanceShouldHonorTaskRunTimeout examines the Timeout behaviour for
// TaskRun level. It creates a TaskRun with Timeout and wait in the Step of the
// inline Task for the time length longer than the specified Timeout.
// The TaskRun is expected to fail with the Reason `TaskRunTimeout`.
func TestConformanceShouldHonorTaskRunTimeout(t *testing.T) {
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

	hasSucceededConditionType := false

	for _, cond := range resolvedTR.Status.Conditions {
		if cond.Type == "Succeeded" {
			if cond.Status != "False" {
				t.Errorf("Expect vendor service to populate Condition `False` but got: %s", cond.Status)
			}
			if cond.Reason != "TaskRunTimeout" {
				t.Errorf("Expect vendor service to populate Condition Reason `TaskRunTimeout` but got: %s", cond.Reason)
			}

			hasSucceededConditionType = true
		}
	}

	if !hasSucceededConditionType {
		t.Errorf("Expect vendor service to populate Succeeded Condition but not apparent in TaskRunStatus")
	}
}

// TestConformanceShouldPopulateConditions examines population of Conditions
// fields. It creates the a TaskRun with minimal specifications and checks the
// required Condition Status and Type.
func TestConformanceShouldPopulateConditions(t *testing.T) {
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
	if len(resolvedTR.Status.Conditions) != 1 {
		t.Errorf("Expect vendor service to populate 1 Condition but no")
	}

	if resolvedTR.Status.Conditions[0].Type != "Succeeded" {
		t.Errorf("Expect vendor service to populate Condition `Succeeded` but got: %s", resolvedTR.Status.Conditions[0].Type)
	}

	if resolvedTR.Status.Conditions[0].Status != "True" {
		t.Errorf("Expect vendor service to populate Condition `True` but got: %s", resolvedTR.Status.Conditions[0].Status)
	}
}

// TestConformanceShouldProvidePipelineTaskParams examines the PipelineTask
// Params functionality by creating a Pipeline that performs addition in its
// Task for validation.
func TestConformanceShouldProvidePipelineTaskParams(t *testing.T) {
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

func TestConformanceShouldProvidePipelineResult(t *testing.T) {
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

func TestConformanceShouldProvidePipelineWorkspace(t *testing.T) {
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

	hasSucceededConditionType := false

	for _, cond := range resolvedPR.Status.Conditions {
		if cond.Type == "Succeeded" {
			if cond.Status != "True" {
				t.Errorf("Expect vendor service to populate Condition `True` but got: %s", cond.Status)
			}
			if cond.Reason != "Succeeded" {
				t.Errorf("Expect vendor service to populate Condition Reason `Succeeded` but got: %s", cond.Reason)
			}
			hasSucceededConditionType = true
		}
	}

	if !hasSucceededConditionType {
		t.Errorf("Expect vendor service to populate Succeeded Condition but not apparent in PipelineRunStatus")
	}

	if resolvedPR.Spec.Workspaces[0].Name != "custom-workspace" {
		t.Errorf("Expect vendor service to provide Workspace 'custom-workspace' but it has: %s", resolvedPR.Spec.Workspaces[0].Name)
	}

	if resolvedPR.Status.PipelineSpec.Workspaces[0].Name != "custom-workspace" {
		t.Errorf("Expect vendor service to provide Workspace 'custom-workspace' in PipelineRun.Status.TaskSpec but it has: %s", resolvedPR.Spec.Workspaces[0].Name)
	}

	// TODO add more tests for WorkSpace Declaration test for PipelineTask Workspace in a separate test
}

func TestConformanceShouldHonorPipelineTaskTimeout(t *testing.T) {
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

	hasSucceededConditionType := false

	for _, cond := range resolvedPR.Status.Conditions {
		if cond.Type == "Succeeded" {
			if cond.Status != "False" {
				t.Errorf("Expect vendor service to populate Condition `False` but got: %s", cond.Status)
			}
			// TODO to examine PipelineRunReason when https://github.com/tektoncd/pipeline/issues/7573 is fixed
			if cond.Reason != "Failed" {
				t.Errorf("Expect vendor service to populate Condition Reason `Failed` but got: %s", cond.Reason)
			}

			hasSucceededConditionType = true
		}
	}

	if !hasSucceededConditionType {
		t.Errorf("Expect vendor service to populate Succeeded Condition but not apparent in PipelineRunStatus")
	}
}

// TestConformanceShouldHonorPipelineRunTimeout examines the Timeout behaviour for
// PipelineRun level. It creates a TaskRun with Timeout and wait in the Step of the
// inline Task for the time length longer than the specified Timeout.
// The TaskRun is expected to fail with the Reason `TaskRunTimeout`.
func TestConformanceShouldHonorPipelineRunTimeout(t *testing.T) {
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

	hasSucceededConditionType := false

	for _, cond := range resolvedPR.Status.Conditions {
		if cond.Type == "Succeeded" {
			if cond.Status != "False" {
				t.Errorf("Expect vendor service to populate Condition `False` but got: %s", cond.Status)
			}
			if cond.Reason != "PipelineRunTimeout" {
				t.Errorf("Expect vendor service to populate Condition Reason `PipelineRunTimeout` but got: %s", cond.Reason)
			}

			hasSucceededConditionType = true
		}
	}

	if !hasSucceededConditionType {
		t.Errorf("Expect vendor service to populate Succeeded Condition but not apparent in PipelineRunStatus")
	}
}

// TestConformancePRShouldPopulateConditions examines population of Conditions
// fields. It creates the a PipelineRun with minimal specifications and checks the
// required Condition Status and Type.
func TestConformancePRShouldPopulateConditions(t *testing.T) {
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
	if len(resolvedPR.Status.Conditions) != 1 {
		t.Errorf("Expect vendor service to populate 1 Condition but no")
	}

	if resolvedPR.Status.Conditions[0].Type != "Succeeded" {
		t.Errorf("Expect vendor service to populate Condition `Succeeded` but got: %s", resolvedPR.Status.Conditions[0].Type)
	}

	if resolvedPR.Status.Conditions[0].Status != "True" {
		t.Errorf("Expect vendor service to populate Condition `True` but got: %s", resolvedPR.Status.Conditions[0].Status)
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

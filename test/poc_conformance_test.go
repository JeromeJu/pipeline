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
      args:
      - echo -n $((${OP1}*${OP2})) | tee $(results.product.path);
`, helpers.ObjectNameForTest(t), strconv.Itoa(multiplicand), strconv.Itoa(multipliper))

	// The execution of Pipeline CRDs that should be implemented by Vendor service
	outputYAML, err := ProcessAndSendToTekton(inputYAML, TaskRunInputType, t)
	if err != nil {
		t.Fatalf("Vendor service failed processing inputYAML: %s", err)
	}

	// Parse and validate output YAML
	resolvedTR := parse.MustParseV1TaskRun(t, outputYAML)
	if len(resolvedTR.Status.Results) != 1 {
		t.Errorf("Expect vendor service to provide 1 result but not")
	}

	if resolvedTR.Status.Results[0].Value.StringVal != strconv.Itoa(multiplicand*multipliper) {
		t.Errorf("Not producing correct result :%s", resolvedTR.Status.Results[0].Value.StringVal)
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
	if len(resolvedTR.Status.Conditions) != 1 {
		t.Errorf("Expect vendor service to populate 1 Condition but no")
	}

	if resolvedTR.Status.Conditions[0].Type != "Succeeded" {
		t.Errorf("Expect vendor service to populate Condition `Succeeded` but got: %s", resolvedTR.Status.Conditions[0].Type)
	}

	if resolvedTR.Status.Conditions[0].Status != "False" {
		t.Errorf("Expect vendor service to populate Condition `False` but got: %s", resolvedTR.Status.Conditions[0].Status)
	}

	if resolvedTR.Status.Conditions[0].Reason != "TaskRunTimeout" {
		t.Errorf("Expect vendor service to populate Condition Reason `TaskRunTimeout` but got: %s", resolvedTR.Status.Conditions[0].Reason)
	}
}

// TestConformanceShouldHonorTaskRunTimeout examines population of Conditions
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

	outputYAML, err := yaml.Marshal(trGot)
	if err != nil {
		return "", err
	}
	return string(outputYAML[:]), nil
}

type VendorService interface {
	CreateTaskRun(ctx context.Context, yaml string) (*v1.TaskRun, error)
	WaitForTaskRun(ctx context.Context, name string) error
	GetTaskRun(ctx context.Context, name string) (*v1.TaskRun, error)
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

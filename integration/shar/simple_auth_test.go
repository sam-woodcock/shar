package intTest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/common/header"
	"gitlab.com/shar-workflow/shar/common/workflow"
	support "gitlab.com/shar-workflow/shar/integration-support"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/errors"
)

// All HS256
const testUserSimpleWorkflowJWT = "eyJhbGciOiJIUzI1NiJ9.eyJpc3MiOiJTaGFySW50ZWdyYXRpb24iLCJ1c2VyIjoiVGVzdFVzZXIiLCJncmFudCI6IlNpbXBsZVdvcmtmbG93VGVzdDpSWFdTIiwiZXhwIjoyNTU0NzMwMDcxLCJpYXQiOjE2NzExMTcyNzEsImF1ZCI6ImdvLXdvcmtmbG93LmNvbSJ9.8eZgpHultcWTgN_mByFn7IufC-LrNThINQ-FoG6bCsU"
const testUserReadOnlyJWT = "eyJhbGciOiJIUzI1NiJ9.eyJpc3MiOiJTaGFySW50ZWdyYXRpb24iLCJ1c2VyIjoiVGVzdFVzZXIiLCJncmFudCI6IlNpbXBsZVdvcmtmbG93VGVzdDpSIiwiZXhwIjoyNTU0NzMwMDcxLCJpYXQiOjE2NzExMTcyNzEsImF1ZCI6ImdvLXdvcmtmbG93LmNvbSJ9.marPR5Cl3EZe9jDGCa3Y8r8q8svOHDKeYaer9SkFwLI"
const randomUserJWT = "eyJhbGciOiJIUzI1NiJ9.eyJpc3MiOiJTaGFySW50ZWdyYXRpb24iLCJ1c2VyIjoiVGVzdFVzZXIiLCJncmFudCI6IlNpbXBsZVdvcmtmbG93VGVzdDpSWFdTIiwiZXhwIjoyNTU0NzMwMDcxLCJpYXQiOjE2NzExMTcyNzEsImF1ZCI6Imp3dC5pbyJ9.0tK1B68thRKXiW6tLvWgGQfZDZjjDv2pM81Hru0toNk"
const testJWTKey = "SuperSecretKey"
const testIssuer = "SharIntegration"
const testAlg = "HS256"
const testAud = "go-workflow.com"

func TestSimpleAuthZ(t *testing.T) {

	tst := &support.Integration{}
	tst.Setup(t, testAuthZFn, testAuthNFn)
	defer tst.Teardown()

	// Create a starting context
	ctx := context.Background()
	ctx = header.ToCtx(ctx, header.Values{"JWT": testUserSimpleWorkflowJWT})
	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	err := cl.Dial(tst.NatsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/simple-workflow.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "SimpleWorkflowTest", b)
	require.NoError(t, err)

	complete := make(chan *model.WorkflowInstanceComplete, 100)

	d := &testSimpleAuthHandlerDef{t: t}

	// Register a service task
	cl.RegisterWorkflowInstanceComplete(complete)
	err = cl.RegisterServiceTask(ctx, "SimpleProcess", d.integrationSimple)
	require.NoError(t, err)

	// Launch the workflow
	if _, _, err := cl.LaunchWorkflow(ctx, "SimpleWorkflowTest", model.Vars{}); err != nil {
		panic(err)
	}

	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()
	select {
	case c := <-complete:
		fmt.Println("completed " + c.WorkflowInstanceId)
	case <-time.After(5 * time.Second):
		require.Fail(t, "Timed out")
	}
	tst.AssertCleanKV()
}

func TestNoAuthN(t *testing.T) {

	tst := &support.Integration{}
	tst.Setup(t, testAuthZFn, testAuthNFn)
	defer tst.Teardown()

	// Create a starting context
	ctx := context.Background()
	ctx = header.ToCtx(ctx, header.Values{"JWT": randomUserJWT})
	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	err := cl.Dial(tst.NatsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/simple-workflow.bpmn")
	fmt.Println("RET1:", err)
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "SimpleWorkflowTest", b)
	assert.ErrorContains(t, err, "failed to authenticate")

}

func TestSimpleNoAuthZ(t *testing.T) {

	tst := &support.Integration{}
	tst.Setup(t, testAuthZFn, testAuthNFn)
	defer tst.Teardown()

	// Create a starting context
	ctx := context.Background()
	ctx = header.ToCtx(ctx, header.Values{"JWT": testUserReadOnlyJWT})
	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	err := cl.Dial(tst.NatsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/simple-workflow.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "SimpleWorkflowTest", b)
	fmt.Println("RET2:", err)
	assert.ErrorContains(t, err, "failed to authorize")

	tst.AssertCleanKV()
}

func testAuthNFn(ctx context.Context, request *model.ApiAuthenticationRequest) (*model.ApiAuthenticationResponse, error) {
	fmt.Println("AUTHN:", request.Headers)
	j := request.Headers["JWT"]
	claims := jwt.MapClaims{}
	if token, err := jwt.ParseWithClaims(j, claims, func(token *jwt.Token) (interface{}, error) { return []byte(testJWTKey), nil }); err != nil {
		return nil, fmt.Errorf("invalid token: %w", errors.ErrApiAuthZFail)
	} else if token.Header["alg"] != testAlg ||
		!claims.VerifyAudience(testAud, true) ||
		!claims.VerifyIssuer(testIssuer, true) ||
		!claims.VerifyExpiresAt(time.Now().Unix(), true) ||
		!claims.VerifyIssuedAt(time.Now().Unix(), true) {
		return nil, fmt.Errorf("invalid token: %w", errors.ErrApiAuthZFail)
	}
	return &model.ApiAuthenticationResponse{Authenticated: true, User: claims["user"].(string)}, nil
}

func testAuthZFn(ctx context.Context, request *model.ApiAuthorizationRequest) (*model.ApiAuthorizationResponse, error) {
	fmt.Println("AUTHZ:", request.Function, request.User, request.Headers)
	j := request.Headers["JWT"]
	start := strings.IndexByte(j, '.') + 1
	end := strings.LastIndexByte(j, '.')
	seg := j[start:end]
	var claims map[string]interface{}
	if js, err := jwt.DecodeSegment(seg); err == nil {
		err := json.Unmarshal(js, &claims)
		if err != nil {
			return nil, fmt.Errorf("invalid token: %w", errors.ErrApiAuthZFail)
		}
	} else {
		if err != nil {
			return nil, fmt.Errorf("invalid token: %w", errors.ErrApiAuthZFail)
		}
	}
	if claims["grant"] == nil {
		return nil, fmt.Errorf("unauthorised: %w", errors.ErrApiAuthZFail)
	}
	wf := make(map[string]map[string]struct{})
	for _, i := range strings.Split(claims["grant"].(string), ",") {
		spl := strings.Split(i, ":")
		sub := make(map[string]struct{})
		for _, j := range spl[1] {
			sub[string(j)] = struct{}{}
		}
		wf[spl[0]] = sub
	}

	return &model.ApiAuthorizationResponse{Authorized: APIauth(request.Function, wf[request.WorkflowName])}, nil
}

func APIauth(api string, permissions map[string]struct{}) bool {
	fmt.Println(api)
	switch api {
	case "WORKFLOW.Api.StoreWorkflow":
		_, ok := permissions["W"]
		return ok
	case "WORKFLOW.Api.GetServiceTaskRoutingID":
		return true
	case "WORKFLOW.Api.LaunchWorkflow":
		_, ok := permissions["X"]
		return ok
	case "WORKFLOW.Api.CompleteServiceTask":
		_, ok := permissions["S"]
		return ok
	default:
		return false
	}
}

type testSimpleAuthHandlerDef struct {
	t *testing.T
}

func (d *testSimpleAuthHandlerDef) integrationSimple(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, workflow.WrappedError) {
	fmt.Println("Hi")
	assert.Equal(d.t, 32768, vars["carried"].(int))
	vars["Success"] = true
	return vars, nil
}

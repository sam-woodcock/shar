// Code generated by mockery v2.14.0. DO NOT EDIT.

package intTest

import (
	context "context"

	mock "github.com/stretchr/testify/mock"

	trace "go.opentelemetry.io/otel/sdk/trace"
)

// MockTelemetry is an autogenerated mock type for the Exporter type
type MockTelemetry struct {
	mock.Mock
}

// ExportSpans provides a mock function with given fields: ctx, spans
func (_m *MockTelemetry) ExportSpans(ctx context.Context, spans []trace.ReadOnlySpan) error {
	ret := _m.Called(ctx, spans)

	var r0 error
	if rf, ok := ret.Get(0).(func(context.Context, []trace.ReadOnlySpan) error); ok {
		r0 = rf(ctx, spans)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

type mockConstructorTestingTNewMockTelemetry interface {
	mock.TestingT
	Cleanup(func())
}

// NewMockTelemetry creates a new instance of MockTelemetry. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
func NewMockTelemetry(t mockConstructorTestingTNewMockTelemetry) *MockTelemetry {
	mock := &MockTelemetry{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}

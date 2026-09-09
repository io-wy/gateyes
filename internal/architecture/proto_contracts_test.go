package architecture_test

import (
	"strings"
	"testing"

	controlv1 "github.com/gateyes/gateway/pkg/control/v1"
	runtimev1 "github.com/gateyes/gateway/pkg/runtime/v1"
	workflowv1 "github.com/gateyes/gateway/pkg/workflow/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestInternalProtoServices(t *testing.T) {
	tests := []struct {
		name    string
		file    protoreflect.FileDescriptor
		service protoreflect.Name
		methods map[protoreflect.Name]bool
	}{
		{
			name:    "control",
			file:    controlv1.File_proto_control_v1_runtime_config_proto,
			service: "RuntimeConfigService",
			methods: map[protoreflect.Name]bool{
				"GetRuntimeSnapshot": false,
				"GetRuntimeRevision": false,
			},
		},
		{
			name:    "runtime",
			file:    runtimev1.File_proto_runtime_v1_inference_runtime_proto,
			service: "InferenceRuntimeService",
			methods: map[protoreflect.Name]bool{
				"Execute":       false,
				"ExecuteStream": true,
			},
		},
		{
			name:    "workflow",
			file:    workflowv1.File_proto_workflow_v1_workflow_proto,
			service: "WorkflowService",
			methods: map[protoreflect.Name]bool{
				"ExecuteBatchItem":  false,
				"GetWorkflowStatus": false,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service := tc.file.Services().ByName(tc.service)
			if service == nil {
				t.Fatalf("service %s is missing", tc.service)
			}
			if service.Methods().Len() != len(tc.methods) {
				t.Fatalf("service %s has %d methods, want %d", tc.service, service.Methods().Len(), len(tc.methods))
			}
			for methodName, serverStreaming := range tc.methods {
				method := service.Methods().ByName(methodName)
				if method == nil {
					t.Errorf("method %s is missing", methodName)
					continue
				}
				if method.IsStreamingClient() {
					t.Errorf("method %s must not be client streaming", methodName)
				}
				if method.IsStreamingServer() != serverStreaming {
					t.Errorf("method %s server streaming = %t, want %t", methodName, method.IsStreamingServer(), serverStreaming)
				}
			}
		})
	}
}

func TestInternalProtoRequiredFields(t *testing.T) {
	tests := []struct {
		name   string
		file   protoreflect.FileDescriptor
		fields map[protoreflect.Name]map[protoreflect.Name]protoreflect.FieldNumber
	}{
		{
			name: "control",
			file: controlv1.File_proto_control_v1_runtime_config_proto,
			fields: map[protoreflect.Name]map[protoreflect.Name]protoreflect.FieldNumber{
				"SchemaVersion":             {"major": 1, "minor": 2},
				"RequestMetadata":           {"request_id": 1, "trace_id": 2, "tenant_id": 3},
				"GetRuntimeSnapshotRequest": {"schema_version": 1, "metadata": 2, "known_revision_id": 3},
				"GetRuntimeSnapshotResponse": {"schema_version": 1, "metadata": 2, "snapshot": 3,
					"not_modified": 4, "error": 5},
				"GetRuntimeRevisionRequest":  {"schema_version": 1, "metadata": 2},
				"GetRuntimeRevisionResponse": {"schema_version": 1, "metadata": 2, "revision": 3, "error": 4},
				"RuntimeSnapshot":            {"revision": 1, "content_type": 2, "payload": 3},
				"RuntimeRevision":            {"revision_id": 1, "generation": 2, "updated_at": 3},
				"ControlErrorDetail":         {"code": 1, "message": 2, "retryable": 3},
			},
		},
		{
			name: "runtime",
			file: runtimev1.File_proto_runtime_v1_inference_runtime_proto,
			fields: map[protoreflect.Name]map[protoreflect.Name]protoreflect.FieldNumber{
				"SchemaVersion": {"major": 1, "minor": 2},
				"TraceMetadata": {"request_id": 1, "trace_id": 2, "span_id": 3, "parent_span_id": 4,
					"tenant_id": 5, "session_id": 6},
				"ExecuteRequest": {"schema_version": 1, "idempotency_key": 2, "metadata": 3, "model": 4,
					"surface": 5, "content_type": 6, "payload": 7},
				"ExecuteStreamRequest": {"schema_version": 1, "idempotency_key": 2, "metadata": 3, "model": 4,
					"surface": 5, "content_type": 6, "payload": 7},
				"ExecuteResponse": {"schema_version": 1, "metadata": 2, "provider": 3, "content_type": 4,
					"payload": 5, "usage": 6, "error": 7},
				"ExecuteStreamResponse": {"schema_version": 1, "metadata": 2, "sequence": 3, "content_type": 4,
					"payload": 5, "terminal": 6, "usage": 7, "error": 8},
				"RuntimeErrorDetail": {"domain": 1, "code": 2, "message": 3, "retryable": 4,
					"provider": 5},
			},
		},
		{
			name: "workflow",
			file: workflowv1.File_proto_workflow_v1_workflow_proto,
			fields: map[protoreflect.Name]map[protoreflect.Name]protoreflect.FieldNumber{
				"SchemaVersion":    {"major": 1, "minor": 2},
				"WorkflowMetadata": {"request_id": 1, "trace_id": 2, "tenant_id": 3},
				"ExecuteBatchItemRequest": {"schema_version": 1, "idempotency_key": 2, "metadata": 3,
					"workflow_id": 4, "item_id": 5, "content_type": 6, "payload": 7},
				"ExecuteBatchItemResponse": {"schema_version": 1, "metadata": 2, "workflow_id": 3,
					"item_id": 4, "status": 5, "content_type": 6, "payload": 7, "error": 8},
				"GetWorkflowStatusRequest": {"schema_version": 1, "metadata": 2, "workflow_id": 3},
				"GetWorkflowStatusResponse": {"schema_version": 1, "metadata": 2, "workflow_id": 3,
					"status": 4, "total_items": 5, "completed_items": 6, "failed_items": 7, "error": 8},
				"WorkflowErrorDetail": {"code": 1, "message": 2, "retryable": 3, "runtime_error": 4},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for messageName, expected := range tc.fields {
				message := tc.file.Messages().ByName(messageName)
				if message == nil {
					t.Errorf("message %s is missing", messageName)
					continue
				}
				for fieldName, fieldNumber := range expected {
					field := message.Fields().ByName(fieldName)
					if field == nil {
						t.Errorf("%s.%s is missing", messageName, fieldName)
						continue
					}
					if field.Number() != fieldNumber {
						t.Errorf("%s.%s uses field number %d, want %d", messageName, fieldName, field.Number(), fieldNumber)
					}
				}
			}
		})
	}
}

func TestInternalProtoEnumZeroValuesAreUnspecified(t *testing.T) {
	files := []protoreflect.FileDescriptor{
		controlv1.File_proto_control_v1_runtime_config_proto,
		runtimev1.File_proto_runtime_v1_inference_runtime_proto,
		workflowv1.File_proto_workflow_v1_workflow_proto,
	}
	for _, file := range files {
		enums := file.Enums()
		for index := 0; index < enums.Len(); index++ {
			enum := enums.Get(index)
			zero := enum.Values().ByNumber(0)
			if zero == nil {
				t.Errorf("%s has no zero value", enum.FullName())
				continue
			}
			if !strings.HasSuffix(string(zero.Name()), "_UNSPECIFIED") {
				t.Errorf("%s zero value is %s, want *_UNSPECIFIED", enum.FullName(), zero.Name())
			}
		}
	}
}

func TestInternalProtoErrorNamespacesAreDistinct(t *testing.T) {
	checks := []struct {
		file       protoreflect.FileDescriptor
		enum       protoreflect.Name
		namePrefix string
	}{
		{controlv1.File_proto_control_v1_runtime_config_proto, "ControlErrorCode", "CONTROL_ERROR_CODE_"},
		{runtimev1.File_proto_runtime_v1_inference_runtime_proto, "RuntimeErrorCode", "RUNTIME_ERROR_CODE_"},
		{workflowv1.File_proto_workflow_v1_workflow_proto, "WorkflowErrorCode", "WORKFLOW_ERROR_CODE_"},
	}
	for _, check := range checks {
		enum := check.file.Enums().ByName(check.enum)
		if enum == nil {
			t.Errorf("enum %s is missing", check.enum)
			continue
		}
		for index := 0; index < enum.Values().Len(); index++ {
			value := enum.Values().Get(index)
			if !strings.HasPrefix(string(value.Name()), check.namePrefix) {
				t.Errorf("%s value %s must use prefix %s", enum.FullName(), value.Name(), check.namePrefix)
			}
		}
	}
}

func TestInternalProtoDomainAndWorkflowStates(t *testing.T) {
	assertEnumValues(t, runtimev1.File_proto_runtime_v1_inference_runtime_proto, "RuntimeErrorDomain", []protoreflect.Name{
		"RUNTIME_ERROR_DOMAIN_UNSPECIFIED",
		"RUNTIME_ERROR_DOMAIN_PROVIDER",
		"RUNTIME_ERROR_DOMAIN_INTERNAL_RPC",
		"RUNTIME_ERROR_DOMAIN_CONFIGURATION",
	})
	assertEnumValues(t, workflowv1.File_proto_workflow_v1_workflow_proto, "WorkflowStatus", []protoreflect.Name{
		"WORKFLOW_STATUS_UNSPECIFIED",
		"WORKFLOW_STATUS_PENDING",
		"WORKFLOW_STATUS_RUNNING",
		"WORKFLOW_STATUS_SUCCEEDED",
		"WORKFLOW_STATUS_PARTIALLY_SUCCEEDED",
		"WORKFLOW_STATUS_FAILED",
		"WORKFLOW_STATUS_CANCELLED",
	})
}

func assertEnumValues(t *testing.T, file protoreflect.FileDescriptor, enumName protoreflect.Name, expected []protoreflect.Name) {
	t.Helper()
	enum := file.Enums().ByName(enumName)
	if enum == nil {
		t.Fatalf("enum %s is missing", enumName)
	}
	if enum.Values().Len() != len(expected) {
		t.Fatalf("enum %s has %d values, want %d", enumName, enum.Values().Len(), len(expected))
	}
	for number, name := range expected {
		value := enum.Values().ByNumber(protoreflect.EnumNumber(number))
		if value == nil || value.Name() != name {
			t.Errorf("enum %s value %d = %v, want %s", enumName, number, value, name)
		}
	}
}

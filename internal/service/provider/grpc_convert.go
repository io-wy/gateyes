package provider

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sugarme/tokenizer"
	"github.com/sugarme/tokenizer/pretrained"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

type vllmGRPCDescriptors struct {
	generateRequest     protoreflect.MessageDescriptor
	generateResponse    protoreflect.MessageDescriptor
	generateStreamChunk protoreflect.MessageDescriptor
	generateComplete    protoreflect.MessageDescriptor
	getTokenizerRequest protoreflect.MessageDescriptor
	getTokenizerChunk   protoreflect.MessageDescriptor
}

type vllmGenerateChunk struct {
	TokenIDs []uint32
	Usage    Usage
}

type vllmGenerateComplete struct {
	OutputIDs    []uint32
	FinishReason string
	Usage        Usage
}

type vllmTokenDecoder struct {
	tokenizer *tokenizer.Tokenizer
}

func (d *vllmTokenDecoder) Decode(ids []uint32) (string, error) {
	if d == nil || d.tokenizer == nil {
		return "", fmt.Errorf("token decoder is not initialized")
	}
	values := make([]int, 0, len(ids))
	for _, id := range ids {
		values = append(values, int(id))
	}
	return d.tokenizer.Decode(values, true), nil
}

var (
	vllmGRPCDescriptorsOnce sync.Once
	vllmGRPCDescriptorsVal  *vllmGRPCDescriptors
	vllmGRPCDescriptorsErr  error
)

func loadVLLMGRPCDescriptors() (*vllmGRPCDescriptors, error) {
	vllmGRPCDescriptorsOnce.Do(func() {
		commonFile := &descriptorpb.FileDescriptorProto{
			Name:    proto.String("common.proto"),
			Package: proto.String("smg.grpc.common"),
			Syntax:  proto.String("proto3"),
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("GetTokenizerRequest")},
				{
					Name: proto.String("GetTokenizerChunk"),
					Field: []*descriptorpb.FieldDescriptorProto{
						bytesField("data", 1),
						stringField("sha256", 2),
					},
				},
			},
		}
		engineFile := &descriptorpb.FileDescriptorProto{
			Name:       proto.String("vllm_engine.proto"),
			Package:    proto.String("vllm.grpc.engine"),
			Syntax:     proto.String("proto3"),
			Dependency: []string{"common.proto"},
			MessageType: []*descriptorpb.DescriptorProto{
				{
					Name: proto.String("SamplingParams"),
					Field: []*descriptorpb.FieldDescriptorProto{
						uint32Field("max_tokens", 8),
						stringField("json_schema", 16),
						boolField("json_object", 20),
					},
				},
				{
					Name: proto.String("GenerateRequest"),
					Field: []*descriptorpb.FieldDescriptorProto{
						stringField("request_id", 1),
						stringField("text", 3),
						messageField("sampling_params", 4, ".vllm.grpc.engine.SamplingParams"),
						boolField("stream", 5),
					},
				},
				{
					Name: proto.String("GenerateStreamChunk"),
					Field: []*descriptorpb.FieldDescriptorProto{
						repeatedUint32Field("token_ids", 1),
						uint32Field("prompt_tokens", 2),
						uint32Field("completion_tokens", 3),
						uint32Field("cached_tokens", 4),
					},
				},
				{
					Name: proto.String("GenerateComplete"),
					Field: []*descriptorpb.FieldDescriptorProto{
						repeatedUint32Field("output_ids", 1),
						stringField("finish_reason", 2),
						uint32Field("prompt_tokens", 3),
						uint32Field("completion_tokens", 4),
						uint32Field("cached_tokens", 5),
					},
				},
				{
					Name: proto.String("GenerateResponse"),
					Field: []*descriptorpb.FieldDescriptorProto{
						messageField("chunk", 1, ".vllm.grpc.engine.GenerateStreamChunk"),
						messageField("complete", 2, ".vllm.grpc.engine.GenerateComplete"),
					},
				},
			},
			Service: []*descriptorpb.ServiceDescriptorProto{
				{
					Name: proto.String("VllmEngine"),
					Method: []*descriptorpb.MethodDescriptorProto{
						{
							Name:            proto.String("Generate"),
							InputType:       proto.String(".vllm.grpc.engine.GenerateRequest"),
							OutputType:      proto.String(".vllm.grpc.engine.GenerateResponse"),
							ServerStreaming: proto.Bool(true),
						},
						{
							Name:            proto.String("GetTokenizer"),
							InputType:       proto.String(".smg.grpc.common.GetTokenizerRequest"),
							OutputType:      proto.String(".smg.grpc.common.GetTokenizerChunk"),
							ServerStreaming: proto.Bool(true),
						},
					},
				},
			},
		}

		files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{
			File: []*descriptorpb.FileDescriptorProto{commonFile, engineFile},
		})
		if err != nil {
			vllmGRPCDescriptorsErr = err
			return
		}

		vllmGRPCDescriptorsVal = &vllmGRPCDescriptors{
			generateRequest:     mustMessageDescriptor(files, "vllm.grpc.engine.GenerateRequest"),
			generateResponse:    mustMessageDescriptor(files, "vllm.grpc.engine.GenerateResponse"),
			generateStreamChunk: mustMessageDescriptor(files, "vllm.grpc.engine.GenerateStreamChunk"),
			generateComplete:    mustMessageDescriptor(files, "vllm.grpc.engine.GenerateComplete"),
			getTokenizerRequest: mustMessageDescriptor(files, "smg.grpc.common.GetTokenizerRequest"),
			getTokenizerChunk:   mustMessageDescriptor(files, "smg.grpc.common.GetTokenizerChunk"),
		}
	})
	return vllmGRPCDescriptorsVal, vllmGRPCDescriptorsErr
}

func buildVLLMGRPCGenerateRequest(descriptors *vllmGRPCDescriptors, req *ResponseRequest) (*dynamicpb.Message, error) {
	prompt := buildVLLMGRPCPrompt(req)
	message := dynamicpb.NewMessage(descriptors.generateRequest)
	setDynamicFieldString(message, "request_id", fmt.Sprintf("gateyes-%d", time.Now().UnixNano()))
	setDynamicFieldString(message, "text", prompt)
	setDynamicFieldBool(message, "stream", req != nil && req.Stream)

	sampling := dynamicpb.NewMessage(descriptors.generateRequest.Fields().ByName("sampling_params").Message())
	if maxTokens := req.RequestedMaxTokens(); maxTokens > 0 {
		setDynamicFieldUint32(sampling, "max_tokens", uint32(maxTokens))
	}
	if schema := marshalVLLMJSONSchema(req); schema != "" {
		setDynamicFieldString(sampling, "json_schema", schema)
	}
	if req != nil && req.OutputFormat != nil && strings.EqualFold(req.OutputFormat.Type, "json_object") {
		setDynamicFieldBool(sampling, "json_object", true)
	}
	message.Set(message.Descriptor().Fields().ByName("sampling_params"), protoreflect.ValueOfMessage(sampling))
	return message, nil
}

func buildVLLMGRPCPrompt(req *ResponseRequest) string {
	if req == nil {
		return ""
	}
	var parts []string
	if req.Options != nil && strings.TrimSpace(req.Options.System) != "" {
		parts = append(parts, "system: "+strings.TrimSpace(req.Options.System))
	}
	for _, message := range req.InputMessages() {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role == "" {
			role = "user"
		}
		text := strings.TrimSpace(collectText(message.Content))
		if text != "" {
			parts = append(parts, role+": "+text)
		}
		for _, call := range message.ToolCalls {
			parts = append(parts, fmt.Sprintf("assistant_tool_call: %s %s", call.Function.Name, call.Function.Arguments))
		}
		if message.ToolCallID != "" && text != "" {
			parts = append(parts, fmt.Sprintf("tool_result[%s]: %s", message.ToolCallID, text))
		}
	}
	return strings.Join(parts, "\n")
}

func marshalVLLMJSONSchema(req *ResponseRequest) string {
	if req == nil || req.OutputFormat == nil {
		return ""
	}
	if len(req.OutputFormat.Schema) > 0 {
		raw, err := json.Marshal(req.OutputFormat.Schema)
		if err == nil {
			return string(raw)
		}
	}
	if nested, ok := req.OutputFormat.Raw["json_schema"].(map[string]any); ok {
		if schema, ok := nested["schema"]; ok {
			raw, err := json.Marshal(schema)
			if err == nil {
				return string(raw)
			}
		}
	}
	return ""
}

func parseVLLMGRPCGenerateResponse(descriptors *vllmGRPCDescriptors, message *dynamicpb.Message) (*vllmGenerateChunk, *vllmGenerateComplete) {
	chunkField := message.Descriptor().Fields().ByName("chunk")
	if chunkField != nil && message.Has(chunkField) {
		chunk := message.Get(chunkField).Message()
		return &vllmGenerateChunk{
			TokenIDs: dynamicUint32List(chunk, "token_ids"),
			Usage:    dynamicUsage(chunk),
		}, nil
	}
	completeField := message.Descriptor().Fields().ByName("complete")
	if completeField != nil && message.Has(completeField) {
		complete := message.Get(completeField).Message()
		return nil, &vllmGenerateComplete{
			OutputIDs:    dynamicUint32List(complete, "output_ids"),
			FinishReason: dynamicString(complete, "finish_reason"),
			Usage:        dynamicUsage(complete),
		}
	}
	return nil, nil
}

func decodeVLLMTextDelta(decoder tokenDecoder, tokenIDs []uint32, previous string) (string, string, error) {
	current, err := decoder.Decode(tokenIDs)
	if err != nil {
		return "", previous, err
	}
	prefix := longestCommonPrefixLen(previous, current)
	return current[prefix:], current, nil
}

func newVLLMTokenDecoder(archive []byte) (tokenDecoder, error) {
	tokenizerPath, cleanup, err := extractTokenizerJSON(archive)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	tok, err := pretrained.FromFile(tokenizerPath)
	if err != nil {
		return nil, err
	}
	return &vllmTokenDecoder{tokenizer: tok}, nil
}

func extractTokenizerJSON(archive []byte) (string, func(), error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return "", nil, err
	}

	tempDir, err := os.MkdirTemp("", "gateyes-vllm-tokenizer-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }

	for _, file := range reader.File {
		if !strings.HasSuffix(strings.ToLower(file.Name), "tokenizer.json") {
			continue
		}
		handle, err := file.Open()
		if err != nil {
			cleanup()
			return "", nil, err
		}
		defer handle.Close()

		data, err := io.ReadAll(handle)
		if err != nil {
			cleanup()
			return "", nil, err
		}
		path := filepath.Join(tempDir, "tokenizer.json")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			cleanup()
			return "", nil, err
		}
		return path, cleanup, nil
	}

	cleanup()
	return "", nil, fmt.Errorf("tokenizer.json not found in tokenizer archive")
}

func verifyTokenizerArchiveSHA(data []byte, expected string) error {
	if strings.TrimSpace(expected) == "" {
		return nil
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); !strings.EqualFold(got, expected) {
		return fmt.Errorf("tokenizer archive sha256 mismatch: got=%s want=%s", got, expected)
	}
	return nil
}

func longestCommonPrefixLen(left, right string) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	index := 0
	for index < limit && left[index] == right[index] {
		index++
	}
	return index
}

func dynamicString(message protoreflect.Message, fieldName protoreflect.Name) string {
	field := message.Descriptor().Fields().ByName(fieldName)
	if field == nil || !message.Has(field) {
		return ""
	}
	return message.Get(field).String()
}

func dynamicUint32(message protoreflect.Message, fieldName protoreflect.Name) uint32 {
	field := message.Descriptor().Fields().ByName(fieldName)
	if field == nil || !message.Has(field) {
		return 0
	}
	return uint32(message.Get(field).Uint())
}

func dynamicUint32List(message protoreflect.Message, fieldName protoreflect.Name) []uint32 {
	field := message.Descriptor().Fields().ByName(fieldName)
	if field == nil || !message.Has(field) {
		return nil
	}
	list := message.Get(field).List()
	values := make([]uint32, 0, list.Len())
	for index := 0; index < list.Len(); index++ {
		values = append(values, uint32(list.Get(index).Uint()))
	}
	return values
}

func dynamicUsage(message protoreflect.Message) Usage {
	usage := Usage{
		PromptTokens:     int(dynamicUint32(message, "prompt_tokens")),
		CompletionTokens: int(dynamicUint32(message, "completion_tokens")),
		CachedTokens:     int(dynamicUint32(message, "cached_tokens")),
	}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	return usage
}

func setDynamicFieldString(message *dynamicpb.Message, fieldName protoreflect.Name, value string) {
	field := message.Descriptor().Fields().ByName(fieldName)
	message.Set(field, protoreflect.ValueOfString(value))
}

func setDynamicFieldUint32(message *dynamicpb.Message, fieldName protoreflect.Name, value uint32) {
	field := message.Descriptor().Fields().ByName(fieldName)
	message.Set(field, protoreflect.ValueOfUint32(value))
}

func setDynamicFieldBool(message *dynamicpb.Message, fieldName protoreflect.Name, value bool) {
	field := message.Descriptor().Fields().ByName(fieldName)
	message.Set(field, protoreflect.ValueOfBool(value))
}

func mustMessageDescriptor(files *protoregistry.Files, name protoreflect.FullName) protoreflect.MessageDescriptor {
	descriptor, err := files.FindDescriptorByName(name)
	if err != nil {
		panic(err)
	}
	return descriptor.(protoreflect.MessageDescriptor)
}

func stringField(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return scalarField(name, number, descriptorpb.FieldDescriptorProto_TYPE_STRING)
}

func bytesField(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return scalarField(name, number, descriptorpb.FieldDescriptorProto_TYPE_BYTES)
}

func uint32Field(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return scalarField(name, number, descriptorpb.FieldDescriptorProto_TYPE_UINT32)
}

func boolField(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return scalarField(name, number, descriptorpb.FieldDescriptorProto_TYPE_BOOL)
}

func repeatedUint32Field(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:   proto.String(name),
		Number: proto.Int32(number),
		Label:  descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
		Type:   descriptorpb.FieldDescriptorProto_TYPE_UINT32.Enum(),
	}
}

func messageField(name string, number int32, typeName string) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:     proto.String(name),
		Number:   proto.Int32(number),
		Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
		TypeName: proto.String(typeName),
	}
}

func scalarField(name string, number int32, fieldType descriptorpb.FieldDescriptorProto_Type) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:   proto.String(name),
		Number: proto.Int32(number),
		Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		Type:   fieldType.Enum(),
	}
}

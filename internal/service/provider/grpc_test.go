package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/gateyes/gateway/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

type fakeTokenDecoder struct {
	values map[string]string
}

func (d *fakeTokenDecoder) Decode(ids []uint32) (string, error) {
	key := tokenKey(ids)
	value, ok := d.values[key]
	if !ok {
		return "", fmt.Errorf("unexpected token ids: %s", key)
	}
	return value, nil
}

type mockVLLMGRPCServer struct {
	t             *testing.T
	descriptors   *vllmGRPCDescriptors
	tokenizerZip  []byte
	tokenizerHash string
	onGenerate    func(*dynamicpb.Message) ([]*dynamicpb.Message, error)
}

type mockVLLMGRPCService interface{}

func TestNewProviderSupportsGRPCVLLMAndRejectsInvalidConfig(t *testing.T) {
	t.Run("valid grpc vllm config", func(t *testing.T) {
		provider, err := newProvider(config.ProviderConfig{
			Name:       "grpc-vllm",
			Type:       "grpc",
			Vendor:     "vllm",
			GRPCTarget: "127.0.0.1:50051",
			Model:      "Qwen/Qwen3-8B",
			Timeout:    5,
		})
		if err != nil {
			t.Fatalf("newProvider(grpc vllm) error: %v", err)
		}
		grpcProvider, ok := provider.(*grpcProvider)
		if !ok {
			t.Fatalf("newProvider(grpc vllm) type = %T, want *grpcProvider", provider)
		}
		if got := grpcProvider.BaseURL(); got != "127.0.0.1:50051" {
			t.Fatalf("grpcProvider.BaseURL() = %q, want grpcTarget", got)
		}
	})

	t.Run("missing target", func(t *testing.T) {
		_, err := newProvider(config.ProviderConfig{
			Name:   "grpc-vllm",
			Type:   "grpc",
			Vendor: "vllm",
		})
		if err == nil || !strings.Contains(err.Error(), "grpcTarget is required") {
			t.Fatalf("newProvider(missing grpcTarget) error = %v, want grpcTarget is required", err)
		}
	})

	t.Run("unsupported vendor", func(t *testing.T) {
		_, err := newProvider(config.ProviderConfig{
			Name:       "grpc-other",
			Type:       "grpc",
			Vendor:     "other",
			GRPCTarget: "127.0.0.1:50051",
		})
		if err == nil || !strings.Contains(err.Error(), "unsupported grpc vendor") {
			t.Fatalf("newProvider(unsupported grpc vendor) error = %v, want unsupported grpc vendor", err)
		}
	})
}

func TestGRPCProviderCreateResponseUsesGenerateAndTokenizerArchive(t *testing.T) {
	descriptors, err := loadVLLMGRPCDescriptors()
	if err != nil {
		t.Fatalf("loadVLLMGRPCDescriptors() error: %v", err)
	}

	tokenizerArchive := []byte("tokenizer-archive")
	server := &mockVLLMGRPCServer{
		t:             t,
		descriptors:   descriptors,
		tokenizerZip:  tokenizerArchive,
		tokenizerHash: sha256Hex(tokenizerArchive),
		onGenerate: func(req *dynamicpb.Message) ([]*dynamicpb.Message, error) {
			if got := getDynamicString(req, "text"); !strings.Contains(got, "system: be concise") || !strings.Contains(got, "user: say hello") {
				t.Fatalf("grpc prompt = %q, want rendered conversation text", got)
			}
			sampling := getDynamicMessage(req, "sampling_params")
			if sampling == nil || getDynamicUint32(sampling, "max_tokens") != 16 {
				t.Fatalf("sampling_params.max_tokens = %d, want 16", getDynamicUint32(sampling, "max_tokens"))
			}
			return []*dynamicpb.Message{
				buildGenerateCompleteMessage(t, descriptors, []uint32{1, 2}, 4, 2, 1, "stop"),
			}, nil
		},
	}

	bufDialer := startMockVLLMGRPCServer(t, server)

	provider := NewGRPCProvider(config.ProviderConfig{
		Name:       "grpc-vllm",
		Type:       "grpc",
		Vendor:     "vllm",
		GRPCTarget: "bufnet",
		Model:      "Qwen/Qwen3-8B",
		Timeout:    5,
		Headers: map[string]string{
			"x-tenant": "default",
		},
	}).(*grpcProvider)
	provider.dialContext = bufDialer
	provider.decoderLoader = func(ctx context.Context, conn *grpc.ClientConn, outgoing metadata.MD) (tokenDecoder, error) {
		got, err := fetchGRPCTokenizerArchive(ctx, conn, outgoing)
		if err != nil {
			return nil, err
		}
		if string(got) != string(tokenizerArchive) {
			return nil, fmt.Errorf("unexpected tokenizer archive: %q", string(got))
		}
		if values := outgoing.Get("x-tenant"); len(values) != 1 || values[0] != "default" {
			return nil, fmt.Errorf("unexpected outgoing metadata: %v", outgoing)
		}
		return &fakeTokenDecoder{values: map[string]string{"1,2": "hello"}}, nil
	}

	resp, err := provider.CreateResponse(context.Background(), &ResponseRequest{
		Model: "public-model",
		Messages: []Message{
			{Role: "user", Content: TextBlocks("say hello")},
		},
		MaxTokens: 16,
		Options: &RequestOptions{
			System: "be concise",
		},
	})
	if err != nil {
		t.Fatalf("grpcProvider.CreateResponse() error: %v", err)
	}
	if got := resp.OutputText(); got != "hello" {
		t.Fatalf("grpcProvider.CreateResponse() text = %q, want %q", got, "hello")
	}
	if resp.Model != "public-model" {
		t.Fatalf("grpcProvider.CreateResponse() model = %q, want public model", resp.Model)
	}
	if resp.Usage.PromptTokens != 4 || resp.Usage.CompletionTokens != 2 || resp.Usage.TotalTokens != 6 || resp.Usage.CachedTokens != 1 {
		t.Fatalf("grpcProvider.CreateResponse() usage = %+v, want prompt/completion/total/cached = 4/2/6/1", resp.Usage)
	}
}

func TestGRPCProviderStreamResponseEmitsDeltasAndCompletedResponse(t *testing.T) {
	descriptors, err := loadVLLMGRPCDescriptors()
	if err != nil {
		t.Fatalf("loadVLLMGRPCDescriptors() error: %v", err)
	}

	server := &mockVLLMGRPCServer{
		t:             t,
		descriptors:   descriptors,
		tokenizerZip:  []byte("tokenizer-archive"),
		tokenizerHash: sha256Hex([]byte("tokenizer-archive")),
		onGenerate: func(req *dynamicpb.Message) ([]*dynamicpb.Message, error) {
			return []*dynamicpb.Message{
				buildGenerateChunkMessage(t, descriptors, []uint32{1}, 3, 1, 0),
				buildGenerateChunkMessage(t, descriptors, []uint32{2}, 3, 2, 0),
				buildGenerateCompleteMessage(t, descriptors, []uint32{1, 2}, 3, 2, 0, "stop"),
			}, nil
		},
	}

	bufDialer := startMockVLLMGRPCServer(t, server)

	provider := NewGRPCProvider(config.ProviderConfig{
		Name:       "grpc-vllm",
		Type:       "grpc",
		Vendor:     "vllm",
		GRPCTarget: "bufnet",
		Model:      "Qwen/Qwen3-8B",
		Timeout:    5,
	}).(*grpcProvider)
	provider.dialContext = bufDialer
	provider.decoderLoader = func(ctx context.Context, conn *grpc.ClientConn, outgoing metadata.MD) (tokenDecoder, error) {
		return &fakeTokenDecoder{values: map[string]string{
			"1":   "h",
			"1,2": "hi",
		}}, nil
	}

	events, errs := provider.StreamResponse(context.Background(), &ResponseRequest{
		Model:    "public-model",
		Messages: []Message{{Role: "user", Content: TextBlocks("say hi")}},
		Stream:   true,
	})

	var got []ResponseEvent
	for event := range events {
		got = append(got, event)
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("grpcProvider.StreamResponse() error: %v", err)
		}
	}

	if len(got) != 3 {
		t.Fatalf("grpcProvider.StreamResponse() events length = %d, want 3", len(got))
	}
	if got[0].Type != EventContentDelta || got[0].Text() != "h" {
		t.Fatalf("first event = %+v, want first text delta", got[0])
	}
	if got[1].Type != EventContentDelta || got[1].Text() != "i" {
		t.Fatalf("second event = %+v, want second text delta", got[1])
	}
	if got[2].Type != EventResponseCompleted || got[2].Response == nil || got[2].Response.OutputText() != "hi" {
		t.Fatalf("completed event = %+v, want completed response with hi", got[2])
	}
}

func TestFetchGRPCTokenizerArchiveRejectsHashMismatch(t *testing.T) {
	descriptors, err := loadVLLMGRPCDescriptors()
	if err != nil {
		t.Fatalf("loadVLLMGRPCDescriptors() error: %v", err)
	}

	server := &mockVLLMGRPCServer{
		t:             t,
		descriptors:   descriptors,
		tokenizerZip:  []byte("bad-archive"),
		tokenizerHash: strings.Repeat("0", 64),
		onGenerate: func(req *dynamicpb.Message) ([]*dynamicpb.Message, error) {
			return nil, nil
		},
	}

	bufDialer := startMockVLLMGRPCServer(t, server)
	conn, err := bufDialer(context.Background(), "bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("buf dial error: %v", err)
	}
	defer conn.Close()

	_, err = fetchGRPCTokenizerArchive(context.Background(), conn, nil)
	if err == nil || !strings.Contains(err.Error(), "tokenizer archive sha256 mismatch") {
		t.Fatalf("fetchGRPCTokenizerArchive(hash mismatch) error = %v, want mismatch error", err)
	}
}

func TestGRPCProviderRejectsUnsupportedRequestShapes(t *testing.T) {
	provider := NewGRPCProvider(config.ProviderConfig{
		Name:       "grpc-vllm",
		Type:       "grpc",
		Vendor:     "vllm",
		GRPCTarget: "127.0.0.1:50051",
		Model:      "Qwen/Qwen3-8B",
		Timeout:    5,
	}).(*grpcProvider)

	t.Run("create response rejects tools", func(t *testing.T) {
		_, err := provider.CreateResponse(context.Background(), &ResponseRequest{
			Model: "public-model",
			Messages: []Message{{
				Role:    "user",
				Content: TextBlocks("hello"),
			}},
			Tools: []any{map[string]any{"type": "function"}},
		})
		if err == nil || !strings.Contains(err.Error(), "does not support tool calls yet") {
			t.Fatalf("CreateResponse(tools) error = %v, want tool-calls-not-supported", err)
		}
	})

	t.Run("stream response rejects image input", func(t *testing.T) {
		events, errs := provider.StreamResponse(context.Background(), &ResponseRequest{
			Model: "public-model",
			Messages: []Message{{
				Role: "user",
				Content: []ContentBlock{{
					Type:  "image",
					Image: &ContentImage{URL: "https://example.com/cat.png"},
				}},
			}},
			Stream: true,
		})
		for range events {
		}
		var err error
		for item := range errs {
			if item != nil {
				err = item
			}
		}
		if err == nil || !strings.Contains(err.Error(), "does not support image inputs yet") {
			t.Fatalf("StreamResponse(image) error = %v, want image-not-supported", err)
		}
	})
}

func TestBuildVLLMGRPCGenerateRequestIncludesStructuredOutput(t *testing.T) {
	descriptors, err := loadVLLMGRPCDescriptors()
	if err != nil {
		t.Fatalf("loadVLLMGRPCDescriptors() error: %v", err)
	}

	request, err := buildVLLMGRPCGenerateRequest(descriptors, &ResponseRequest{
		Model: "public-model",
		Messages: []Message{{
			Role:    "user",
			Content: TextBlocks("return structured output"),
		}},
		MaxOutputTokens: 24,
		OutputFormat: &OutputFormat{
			Type:   "json_schema",
			Name:   "WeatherResponse",
			Strict: true,
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"status": map[string]any{"type": "string"},
				},
			},
		},
		Options: &RequestOptions{
			System: "json only",
		},
	})
	if err != nil {
		t.Fatalf("buildVLLMGRPCGenerateRequest() error: %v", err)
	}

	if got := getDynamicString(request, "text"); !strings.Contains(got, "system: json only") || !strings.Contains(got, "user: return structured output") {
		t.Fatalf("grpc prompt = %q, want system and user text", got)
	}
	sampling := getDynamicMessage(request, "sampling_params")
	if sampling == nil {
		t.Fatal("sampling_params = nil, want message")
	}
	if got := getDynamicUint32(sampling, "max_tokens"); got != 24 {
		t.Fatalf("sampling_params.max_tokens = %d, want 24", got)
	}
	schema := getDynamicString(sampling, "json_schema")
	if !strings.Contains(schema, `"type":"object"`) || !strings.Contains(schema, `"status"`) {
		t.Fatalf("sampling_params.json_schema = %q, want marshaled schema", schema)
	}
}

func TestGRPCErrorMapsStatusCodesToUpstreamErrors(t *testing.T) {
	t.Run("resource exhausted", func(t *testing.T) {
		err := grpcError(status.Error(codes.ResourceExhausted, "too many requests"))
		upstream, ok := err.(*UpstreamError)
		if !ok {
			t.Fatalf("grpcError(ResourceExhausted) type = %T, want *UpstreamError", err)
		}
		if upstream.StatusCode != 429 || upstream.Message != "too many requests" {
			t.Fatalf("grpcError(ResourceExhausted) = %+v, want 429 and message", upstream)
		}
	})

	t.Run("invalid argument", func(t *testing.T) {
		err := grpcError(status.Error(codes.InvalidArgument, "bad request"))
		upstream, ok := err.(*UpstreamError)
		if !ok {
			t.Fatalf("grpcError(InvalidArgument) type = %T, want *UpstreamError", err)
		}
		if upstream.StatusCode != 400 || upstream.Message != "bad request" {
			t.Fatalf("grpcError(InvalidArgument) = %+v, want 400 and message", upstream)
		}
	})
}

func startMockVLLMGRPCServer(t *testing.T, server *mockVLLMGRPCServer) grpcDialContextFunc {
	t.Helper()

	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	grpcServer.RegisterService(&grpc.ServiceDesc{
		ServiceName: "vllm.grpc.engine.VllmEngine",
		HandlerType: (*mockVLLMGRPCService)(nil),
		Streams: []grpc.StreamDesc{
			{
				StreamName:    "Generate",
				Handler:       func(srv any, stream grpc.ServerStream) error { return srv.(*mockVLLMGRPCServer).handleGenerate(stream) },
				ServerStreams: true,
			},
			{
				StreamName: "GetTokenizer",
				Handler: func(srv any, stream grpc.ServerStream) error {
					return srv.(*mockVLLMGRPCServer).handleGetTokenizer(stream)
				},
				ServerStreams: true,
			},
		},
	}, server)

	go func() {
		if err := grpcServer.Serve(listener); err != nil && !strings.Contains(err.Error(), "closed") {
			t.Errorf("grpcServer.Serve() error: %v", err)
		}
	}()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
	})

	return func(ctx context.Context, target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
		opts = append(opts, grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}))
		return grpc.DialContext(ctx, "bufnet", opts...)
	}
}

func (s *mockVLLMGRPCServer) handleGenerate(stream grpc.ServerStream) error {
	request := dynamicpb.NewMessage(s.descriptors.generateRequest)
	if err := stream.RecvMsg(request); err != nil {
		return err
	}
	responses, err := s.onGenerate(request)
	if err != nil {
		return err
	}
	for _, response := range responses {
		if err := stream.SendMsg(response); err != nil {
			return err
		}
	}
	return nil
}

func (s *mockVLLMGRPCServer) handleGetTokenizer(stream grpc.ServerStream) error {
	request := dynamicpb.NewMessage(s.descriptors.getTokenizerRequest)
	if err := stream.RecvMsg(request); err != nil {
		return err
	}
	chunk := dynamicpb.NewMessage(s.descriptors.getTokenizerChunk)
	setDynamicBytes(chunk, "data", s.tokenizerZip)
	setDynamicString(chunk, "sha256", s.tokenizerHash)
	return stream.SendMsg(chunk)
}

func buildGenerateChunkMessage(t *testing.T, descriptors *vllmGRPCDescriptors, tokenIDs []uint32, promptTokens, completionTokens, cachedTokens uint32) *dynamicpb.Message {
	t.Helper()
	chunk := dynamicpb.NewMessage(descriptors.generateStreamChunk)
	setDynamicUint32List(chunk, "token_ids", tokenIDs)
	setDynamicUint32(chunk, "prompt_tokens", promptTokens)
	setDynamicUint32(chunk, "completion_tokens", completionTokens)
	setDynamicUint32(chunk, "cached_tokens", cachedTokens)

	response := dynamicpb.NewMessage(descriptors.generateResponse)
	setDynamicMessage(response, "chunk", chunk)
	return response
}

func buildGenerateCompleteMessage(t *testing.T, descriptors *vllmGRPCDescriptors, outputIDs []uint32, promptTokens, completionTokens, cachedTokens uint32, finishReason string) *dynamicpb.Message {
	t.Helper()
	complete := dynamicpb.NewMessage(descriptors.generateComplete)
	setDynamicUint32List(complete, "output_ids", outputIDs)
	setDynamicString(complete, "finish_reason", finishReason)
	setDynamicUint32(complete, "prompt_tokens", promptTokens)
	setDynamicUint32(complete, "completion_tokens", completionTokens)
	setDynamicUint32(complete, "cached_tokens", cachedTokens)

	response := dynamicpb.NewMessage(descriptors.generateResponse)
	setDynamicMessage(response, "complete", complete)
	return response
}

func getDynamicMessage(message *dynamicpb.Message, fieldName protoreflect.Name) *dynamicpb.Message {
	field := message.Descriptor().Fields().ByName(fieldName)
	if field == nil || !message.Has(field) {
		return nil
	}
	value := message.Get(field).Message()
	if value == nil {
		return nil
	}
	dynamicMessage, _ := value.Interface().(*dynamicpb.Message)
	return dynamicMessage
}

func getDynamicString(message *dynamicpb.Message, fieldName protoreflect.Name) string {
	field := message.Descriptor().Fields().ByName(fieldName)
	if field == nil || !message.Has(field) {
		return ""
	}
	return message.Get(field).String()
}

func getDynamicUint32(message *dynamicpb.Message, fieldName protoreflect.Name) uint32 {
	field := message.Descriptor().Fields().ByName(fieldName)
	if field == nil || !message.Has(field) {
		return 0
	}
	return uint32(message.Get(field).Uint())
}

func setDynamicMessage(message *dynamicpb.Message, fieldName protoreflect.Name, nested *dynamicpb.Message) {
	field := message.Descriptor().Fields().ByName(fieldName)
	message.Set(field, protoreflect.ValueOfMessage(nested))
}

func setDynamicString(message *dynamicpb.Message, fieldName protoreflect.Name, value string) {
	field := message.Descriptor().Fields().ByName(fieldName)
	message.Set(field, protoreflect.ValueOfString(value))
}

func setDynamicBytes(message *dynamicpb.Message, fieldName protoreflect.Name, value []byte) {
	field := message.Descriptor().Fields().ByName(fieldName)
	message.Set(field, protoreflect.ValueOfBytes(value))
}

func setDynamicUint32(message *dynamicpb.Message, fieldName protoreflect.Name, value uint32) {
	field := message.Descriptor().Fields().ByName(fieldName)
	message.Set(field, protoreflect.ValueOfUint32(value))
}

func setDynamicUint32List(message *dynamicpb.Message, fieldName protoreflect.Name, values []uint32) {
	field := message.Descriptor().Fields().ByName(fieldName)
	list := message.Mutable(field).List()
	for _, value := range values {
		list.Append(protoreflect.ValueOfUint32(value))
	}
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func tokenKey(ids []uint32) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("%d", id))
	}
	return strings.Join(parts, ",")
}

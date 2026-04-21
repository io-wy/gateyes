package provider

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gateyes/gateway/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/dynamicpb"
)

type grpcDialContextFunc func(ctx context.Context, target string, opts ...grpc.DialOption) (*grpc.ClientConn, error)

type grpcTokenDecoderLoader func(ctx context.Context, conn *grpc.ClientConn, outgoing metadata.MD) (tokenDecoder, error)

type tokenDecoder interface {
	Decode(ids []uint32) (string, error)
}

type grpcProvider struct {
	baseProvider

	dialContext   grpcDialContextFunc
	decoderLoader grpcTokenDecoderLoader

	mu      sync.Mutex
	conn    *grpc.ClientConn
	decoder tokenDecoder
}

func NewGRPCProvider(cfg config.ProviderConfig) Provider {
	return &grpcProvider{
		baseProvider:  baseProvider{cfg: cfg},
		dialContext:   grpc.DialContext,
		decoderLoader: defaultGRPCDecoderLoader,
	}
}

func (p *grpcProvider) CreateResponse(ctx context.Context, req *ResponseRequest) (*Response, error) {
	if err := p.validateRequest(req); err != nil {
		return nil, err
	}

	callCtx, cancel := providerCallContext(ctx, p.cfg.Timeout)
	defer cancel()

	descriptors, err := loadVLLMGRPCDescriptors()
	if err != nil {
		return nil, newProviderConfigError("provider.grpc.descriptors", err.Error())
	}
	conn, outgoing, err := p.prepareCall(callCtx)
	if err != nil {
		return nil, err
	}
	decoder, err := p.getDecoder(callCtx, conn, outgoing)
	if err != nil {
		return nil, err
	}
	request, err := buildVLLMGRPCGenerateRequest(descriptors, req)
	if err != nil {
		return nil, newProviderConfigError("provider.grpc.build_request", err.Error())
	}

	stream, err := openVLLMGRPCGenerateStream(callCtx, conn, outgoing, request)
	if err != nil {
		return nil, err
	}

	outputIDs, usage, _, err := collectVLLMGenerateOutput(stream, descriptors)
	if err != nil {
		return nil, err
	}
	text, err := decoder.Decode(outputIDs)
	if err != nil {
		return nil, newProviderParseError("provider.grpc.decode_response", err, "decode vllm grpc output tokens")
	}

	return NewTextResponse("", req.Model, text, usage), nil
}

func (p *grpcProvider) prepareCall(ctx context.Context) (*grpc.ClientConn, metadata.MD, error) {
	conn, err := p.getConn(ctx)
	if err != nil {
		return nil, nil, err
	}
	return conn, p.outgoingMetadata(), nil
}

func (p *grpcProvider) getConn(ctx context.Context) (*grpc.ClientConn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.conn != nil {
		return p.conn, nil
	}
	conn, err := p.dialContext(ctx, strings.TrimSpace(p.cfg.GRPCTarget), p.grpcDialOptions()...)
	if err != nil {
		return nil, newProviderTransportError("provider.grpc.dial", err)
	}
	p.conn = conn
	return conn, nil
}

func (p *grpcProvider) getDecoder(ctx context.Context, conn *grpc.ClientConn, outgoing metadata.MD) (tokenDecoder, error) {
	p.mu.Lock()
	if p.decoder != nil {
		defer p.mu.Unlock()
		return p.decoder, nil
	}
	p.mu.Unlock()

	decoder, err := p.decoderLoader(ctx, conn, outgoing)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.decoder == nil {
		p.decoder = decoder
	}
	return p.decoder, nil
}

func (p *grpcProvider) grpcDialOptions() []grpc.DialOption {
	opts := []grpc.DialOption{
		grpc.WithBlock(),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(128 << 20)),
	}
	if p.cfg.GRPCUseTLS {
		tlsConfig := &tls.Config{}
		if authority := strings.TrimSpace(p.cfg.GRPCAuthority); authority != "" {
			tlsConfig.ServerName = authority
		}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
		return opts
	}
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	return opts
}

func (p *grpcProvider) outgoingMetadata() metadata.MD {
	md := metadata.MD{}
	for key, value := range p.cfg.Headers {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		md.Set(key, value)
	}
	if strings.TrimSpace(p.cfg.APIKey) != "" && len(md.Get("authorization")) == 0 {
		md.Set("authorization", "Bearer "+strings.TrimSpace(p.cfg.APIKey))
	}
	return md
}

func (p *grpcProvider) validateRequest(req *ResponseRequest) error {
	if req == nil {
		return newUpstreamError(http.StatusBadRequest, "grpc provider request cannot be nil")
	}
	if req.HasToolsRequested() {
		return newUpstreamError(http.StatusBadRequest, "grpc vllm provider does not support tool calls yet")
	}
	if req.HasImageInput() {
		return newUpstreamError(http.StatusBadRequest, "grpc vllm provider does not support image inputs yet")
	}
	return nil
}

func (p *grpcProvider) CloseIdleConnections() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn == nil {
		return
	}
	_ = p.conn.Close()
	p.conn = nil
	p.decoder = nil
}

func defaultGRPCDecoderLoader(ctx context.Context, conn *grpc.ClientConn, outgoing metadata.MD) (tokenDecoder, error) {
	archive, err := fetchGRPCTokenizerArchive(ctx, conn, outgoing)
	if err != nil {
		return nil, err
	}
	decoder, err := newVLLMTokenDecoder(archive)
	if err != nil {
		return nil, newProviderParseError("provider.grpc.load_tokenizer", err, "load vllm tokenizer archive")
	}
	return decoder, nil
}

func fetchGRPCTokenizerArchive(ctx context.Context, conn *grpc.ClientConn, outgoing metadata.MD) ([]byte, error) {
	descriptors, err := loadVLLMGRPCDescriptors()
	if err != nil {
		return nil, err
	}
	streamDesc := &grpc.StreamDesc{ServerStreams: true}
	callCtx := ctx
	if len(outgoing) > 0 {
		callCtx = metadata.NewOutgoingContext(ctx, outgoing)
	}
	stream, err := conn.NewStream(callCtx, streamDesc, "/vllm.grpc.engine.VllmEngine/GetTokenizer")
	if err != nil {
		return nil, grpcError(err)
	}
	request := dynamicpb.NewMessage(descriptors.getTokenizerRequest)
	if err := stream.SendMsg(request); err != nil {
		return nil, grpcError(err)
	}
	if err := stream.CloseSend(); err != nil {
		return nil, grpcError(err)
	}

	var archive []byte
	var expectedSHA string
	for {
		chunk := dynamicpb.NewMessage(descriptors.getTokenizerChunk)
		if err := stream.RecvMsg(chunk); err != nil {
			if status.Code(err) == codes.OutOfRange || strings.Contains(strings.ToLower(err.Error()), "eof") {
				break
			}
			if err == io.EOF {
				break
			}
			return nil, grpcError(err)
		}
		archive = append(archive, chunk.Get(chunk.Descriptor().Fields().ByName("data")).Bytes()...)
		if sha := dynamicString(chunk, "sha256"); sha != "" {
			expectedSHA = sha
		}
	}
	if err := verifyTokenizerArchiveSHA(archive, expectedSHA); err != nil {
		return nil, err
	}
	return archive, nil
}

func providerCallContext(ctx context.Context, timeoutSeconds int) (context.Context, context.CancelFunc) {
	if timeoutSeconds <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
}

func grpcError(err error) error {
	if err == nil {
		return nil
	}
	if err == io.EOF {
		return err
	}
	if st, ok := status.FromError(err); ok {
		return newUpstreamError(grpcStatusCodeToHTTP(st.Code()), st.Message())
	}
	return newProviderTransportError("provider.grpc.transport", err)
}

func grpcStatusCodeToHTTP(code codes.Code) int {
	switch code {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.Unimplemented:
		return http.StatusBadRequest
	case codes.Unauthenticated:
		return http.StatusUnauthorized
	case codes.PermissionDenied:
		return http.StatusForbidden
	case codes.NotFound:
		return http.StatusNotFound
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout
	case codes.Unavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusBadGateway
	}
}

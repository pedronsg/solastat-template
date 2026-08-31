package pluginclient

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/pedronsg/solastat-template/proto/gen"
)

// fakeHub is a minimal PluginHub server: it hands back whatever keys are
// set on it and records the last RegisterRequest it received, so tests can
// assert on what the client reported.
type fakeHub struct {
	pb.UnimplementedPluginHubServer
	keys []string

	lastReq *pb.RegisterRequest
}

func (f *fakeHub) Register(_ context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	f.lastReq = req
	return &pb.RegisterResponse{Keys: f.keys}, nil
}

func (f *fakeHub) SubscribeSolarData(_ *pb.SubscribeRequest, stream pb.PluginHub_SubscribeSolarDataServer) error {
	<-stream.Context().Done()
	return nil
}

func startFakeHub(t *testing.T, hub *fakeHub) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	pb.RegisterPluginHubServer(srv, hub)
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// TestCheckAuthorizedDrivesLocalVerdict exercises Client.register directly
// (bypassing the background connect loop) against a fake server: it must
// call checkAuthorized with whatever keys the server returns, use only that
// return value as the authorization verdict (never anything the server
// claims), and report the previous verdict back in the next request.
func TestCheckAuthorizedDrivesLocalVerdict(t *testing.T) {
	hub := &fakeHub{keys: []string{"key-for-relay"}}
	conn := startFakeHub(t, hub)

	var checkedWith []string
	verdict := false
	c := &Client{
		pluginID:      "relay",
		pluginVersion: "1.0.0",
		checkAuthorized: func(keys []string) bool {
			checkedWith = keys
			return verdict
		},
		conn: conn,
		hub:  pb.NewPluginHubClient(conn),
		done: make(chan struct{}),
	}

	// First register: server has a key, but checkAuthorized says no (wrong
	// device/plugin) — client must stay unauthorized regardless of what the
	// server thinks, and must report Authorized=false (its prior state).
	if got := c.register(); got {
		t.Fatal("expected unauthorized when checkAuthorized returns false")
	}
	if len(checkedWith) != 1 || checkedWith[0] != "key-for-relay" {
		t.Fatalf("checkAuthorized got %v, want the server's key pool", checkedWith)
	}
	if hub.lastReq.Authorized {
		t.Fatal("first RegisterRequest must report the prior (false) verdict")
	}

	// Now checkAuthorized would accept the key (e.g. it verified locally).
	verdict = true
	fired := false
	c.OnAuthorization(func(ok bool) {
		if ok {
			fired = true
		}
	})
	if got := c.register(); !got {
		t.Fatal("expected authorized once checkAuthorized returns true")
	}
	if !fired {
		t.Fatal("OnAuthorization callback did not fire on verdict change")
	}

	// Next register call must now report Authorized=true — the core only
	// ever learns the plugin's own already-decided verdict.
	c.register()
	if !hub.lastReq.Authorized {
		t.Fatal("subsequent RegisterRequest must report the now-true verdict")
	}
}

func TestRegisterWithoutCheckAuthorizedStaysUnauthorized(t *testing.T) {
	hub := &fakeHub{keys: []string{"some-key"}}
	conn := startFakeHub(t, hub)

	c := &Client{
		pluginID: "gridcharge",
		conn:     conn,
		hub:      pb.NewPluginHubClient(conn),
		done:     make(chan struct{}),
	}
	if got := c.register(); got {
		t.Fatal("a nil checkAuthorized must never authorize")
	}
}

func TestRegisterUnreachableHubKeepsPriorVerdict(t *testing.T) {
	c := &Client{pluginID: "relay", done: make(chan struct{})}
	if got := c.register(); got {
		t.Fatal("no hub connection must report unauthorized, not panic")
	}
}

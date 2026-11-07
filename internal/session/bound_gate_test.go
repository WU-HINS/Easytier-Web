package session

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/pb/web"
)

// F-274: the official client asks for features before it identifies itself, and
// the console refuses that call. Refusing is load-bearing - answering early made
// 2.6.4 and 2.7.0 stop connecting - so the refusal is pinned here instead of
// living only in a comment.
func TestFeatureCallIsRefusedUntilTheSessionIsBound(t *testing.T) {
	session := &Session{}
	gated := session.boundOnly(getFeatureHandler)

	if _, err := gated(context.Background(), nil); err == nil {
		t.Fatal("an unbound session answered the feature call")
	} else if !strings.Contains(err.Error(), "not bound yet") {
		t.Fatalf("unexpected refusal: %v", err)
	}

	session.mu.Lock()
	session.bound = true
	session.mu.Unlock()

	body, err := gated(context.Background(), nil)
	if err != nil {
		t.Fatalf("a bound session was refused: %v", err)
	}
	var response web.GetFeatureResponse
	if err := proto.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.GetSupportEncryption() {
		t.Fatal("the feature response should advertise encryption support")
	}
}

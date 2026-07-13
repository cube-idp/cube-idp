package apply

import "testing"

func TestParseMultiDoc(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantLen int
		wantErr bool
	}{
		{
			name: "two-doc YAML stream",
			data: []byte(`apiVersion: v1
kind: Namespace
metadata:
  name: test-ns
---
apiVersion: v1
kind: Service
metadata:
  name: test-svc
  namespace: test-ns
`),
			wantLen: 2,
			wantErr: false,
		},
		{
			name: "stream with empty documents",
			data: []byte(`apiVersion: v1
kind: Namespace
metadata:
  name: test-ns
---
---
apiVersion: v1
kind: Service
metadata:
  name: test-svc
  namespace: test-ns
`),
			wantLen: 2,
			wantErr: false,
		},
		{
			name: "comment-only docs are skipped",
			data: []byte(`apiVersion: v1
kind: Namespace
metadata:
  name: test-ns
---
# This is just a comment
---
apiVersion: v1
kind: Service
metadata:
  name: test-svc
  namespace: test-ns
`),
			wantLen: 2,
			wantErr: false,
		},
		{
			name:    "malformed YAML",
			data:    []byte(`invalid: yaml: content: here:`),
			wantLen: 0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objs, err := ParseMultiDoc(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseMultiDoc() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(objs) != tt.wantLen {
				t.Errorf("ParseMultiDoc() got %d objects, want %d", len(objs), tt.wantLen)
			}
		})
	}
}

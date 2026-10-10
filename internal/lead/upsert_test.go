package lead

import (
	"testing"

	"github.com/google/uuid"
)

func TestDecide(t *testing.T) {
	in := Incoming{ExternalID: "p-1", Fields: Fields{Name: "Ada", Email: "ada@example.com", Company: "Acme", Source: "web"}}
	a := &Record{ID: uuid.New(), Fields: Fields{Name: "Ada", Email: "ada@example.com", Company: "Acme", Source: "web"}}
	renamed := &Record{ID: a.ID, Fields: Fields{Name: "Old", Email: "ada@example.com", Company: "Acme", Source: "web"}}
	b := &Record{ID: uuid.New(), Fields: Fields{Name: "Grace", Email: "grace@example.com", Company: "Navy", Source: "web"}}

	cases := []struct {
		name          string
		byID, byEmail *Record
		want          Action
		wantTarget    *Record
	}{
		{"new lead", nil, nil, Create, nil},
		{"same id, same fields", a, a, Unchanged, a},
		{"same id, changed name", renamed, renamed, Update, renamed},
		{"new id, existing email", nil, renamed, Update, renamed},
		{"known id, email now unused", renamed, nil, Update, renamed},
		{"id and email belong to different leads", a, b, Conflict, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, target := Decide(c.byID, c.byEmail, in)
			if got != c.want || target != c.wantTarget {
				t.Fatalf("Decide = %v, %v; want %v, %v", got, target, c.want, c.wantTarget)
			}
		})
	}
}

func TestDecideIgnoresEmailCase(t *testing.T) {
	existing := &Record{ID: uuid.New(), Fields: Fields{Name: "Ada", Email: "ada@example.com", Company: "Acme", Source: "web"}}
	in := Incoming{ExternalID: "p-1", Fields: Fields{Name: "Ada", Email: "ADA@Example.com", Company: "Acme", Source: "web"}}
	if got, _ := Decide(existing, existing, in); got != Unchanged {
		t.Fatalf("Decide = %v, want Unchanged", got)
	}
}

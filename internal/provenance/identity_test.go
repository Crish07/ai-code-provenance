package provenance

import "testing"

func TestIdentities(t *testing.T) {
	v := Identities([]string{"x", "x", "y"})
	if v[0].Occurrence != 1 || v[1].Occurrence != 2 || v[0].Hash == v[1].Hash || v[0].Before != "" || v[2].After != "" {
		t.Fatalf("%#v", v)
	}
	if v[0].Hash != Identities([]string{"x", "x", "y"})[0].Hash {
		t.Fatal("unstable")
	}
}
func TestMigrate(t *testing.T) {
	got := Migrate([]string{"old", "keep", "keep"}, []string{"new", "keep", "changed"}, []Source{Unknown, AI, Unknown})
	if got[0] != AI || got[1] != AI || got[2] != AI {
		t.Fatalf("%#v", got)
	}
}
func TestMigrateWithRemovals(t *testing.T) {
	v := MigrateWithRemovals([]string{"a", "b"}, []string{"b"}, []Source{AI, Unknown})
	if len(v.Removed) != 1 || v.Removed[0] != 0 || v.Sources[0] != Unknown {
		t.Fatalf("%#v", v)
	}
}
func TestMigrate_RewriteDoesNotInherit(t *testing.T) {
	v := Migrate([]string{"ai"}, []string{"human rewrite"}, []Source{AI})
	if v[0] != AI {
		t.Fatal(v)
	}
}

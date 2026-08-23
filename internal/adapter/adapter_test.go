package adapter

import "testing"

func TestBuildEngine_ExplicitAdapterOrder(t *testing.T) {
	_, loaded, err := BuildEngine()
	if err != nil {
		t.Fatalf("BuildEngine() error = %v", err)
	}

	want := []LoadedAdapter{
		{ID: "vrchat.core", Origin: "core"},
		{ID: "community.yamaplayer", Origin: "community"},
		{ID: "community.iwasync3", Origin: "community"},
	}
	if len(loaded) != len(want) {
		t.Fatalf("loaded adapters = %d, want %d: %+v", len(loaded), len(want), loaded)
	}
	for i, w := range want {
		if loaded[i] != w {
			t.Errorf("loaded[%d] = %+v, want %+v", i, loaded[i], w)
		}
	}
}

package common

import (
	"bytes"
	"testing"
	"time"

	"github.com/maypok86/otter/v2"
)

func TestCacheGobDecodesStructWithAddedField(t *testing.T) {
	type oldProperty struct {
		ID   int32
		Name string
	}
	type newProperty struct {
		ID      int32
		Name    string
		Enabled bool
	}

	oldCache, err := otter.New(&otter.Options[string, oldProperty]{MaximumSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	oldCache.Set("property", oldProperty{ID: 42, Name: "example"})

	var data bytes.Buffer
	if _, err := SaveCacheToWriter(t.Context(), &data, oldCache, 1, nil); err != nil {
		t.Fatal(err)
	}

	newCache, err := otter.New(&otter.Options[string, newProperty]{MaximumSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := LoadCacheFromReader(t.Context(), &data, newCache, time.Hour); err != nil {
		t.Fatal(err)
	}

	property, ok := newCache.GetIfPresent("property")
	if !ok {
		t.Fatal("property was not restored")
	}
	if property.ID != 42 || property.Name != "example" || property.Enabled {
		t.Fatalf("restored property = %+v", property)
	}
}

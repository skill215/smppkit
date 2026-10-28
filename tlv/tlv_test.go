package tlv_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/skill215/smppkit/tlv"
)

func TestFieldsOwnershipAndOrder(t *testing.T) {
	input := []byte{1, 2}
	var fields tlv.Fields
	fields.Append(0x1500, input)
	fields.Append(0x1501, []byte{3})
	fields.Append(0x1500, []byte{4})
	input[0] = 9
	first, ok := fields.First(0x1500)
	if !ok || !bytes.Equal(first, []byte{1, 2}) {
		t.Fatal(fields)
	}
	first[0] = 5 // accessors intentionally borrow values
	all := fields.All(0x1500)
	if !reflect.DeepEqual(all, [][]byte{{5, 2}, {4}}) {
		t.Fatal(all)
	}
	all[1][0] = 6
	copy := fields.Clone()
	copy[0].Value[0] = 7
	copy[2].Value[0] = 8
	if !reflect.DeepEqual(fields.All(0x1500), [][]byte{{5, 2}, {6}}) {
		t.Fatal("clone aliases values")
	}
	replacement := []byte{10}
	if !fields.ReplaceFirst(0x1500, replacement) || fields.ReplaceFirst(0x1502, replacement) {
		t.Fatal("replace presence")
	}
	replacement[0] = 11
	if !reflect.DeepEqual(fields, tlv.Fields{{Tag: 0x1500, Value: []byte{10}}, {Tag: 0x1501, Value: []byte{3}}, {Tag: 0x1500, Value: []byte{6}}}) {
		t.Fatal(fields)
	}
	fields.DeleteAll(0x1500)
	if len(fields) != 1 || fields[0].Tag != 0x1501 || fields[0].Value[0] != 3 {
		t.Fatal(fields)
	}
	if _, ok := fields.First(0x1500); ok || len(fields.All(0x1500)) != 0 {
		t.Fatal("deleted occurrences remain")
	}
	if (tlv.Fields(nil)).Clone() != nil {
		t.Fatal("nil clone")
	}
	empty := (tlv.Fields{{Value: nil}, {Value: []byte{}}}).Clone()
	if empty[0].Value != nil || empty[1].Value == nil {
		t.Fatal("nil/empty clone distinction lost")
	}
}

func TestUint16NetworkOrderAndWidth(t *testing.T) {
	f := tlv.Uint16(0x1500, 0xabcd)
	if f.Tag != 0x1500 || !bytes.Equal(f.Value, []byte{0xab, 0xcd}) {
		t.Fatal(f)
	}
	if n, err := f.Uint16(); err != nil || n != 0xabcd {
		t.Fatal(n, err)
	}
	for _, b := range [][]byte{nil, {0xab}, {0xab, 0xcd, 0}} {
		if _, err := (tlv.Field{Value: b}).Uint16(); err == nil {
			t.Fatalf("accepted width %d", len(b))
		}
	}
}

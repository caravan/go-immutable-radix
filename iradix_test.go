package iradix

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"testing/quick"

	"github.com/hashicorp/go-uuid"
)

func CopyTree(t *Tree) *Tree {
	nt := &Tree{
		root: CopyNode(t.root),
	}
	return nt
}

func CopyNode(n *Node) *Node {
	nn := &Node{}
	if n.prefix != nil {
		nn.prefix = make([]byte, len(n.prefix))
		copy(nn.prefix, n.prefix)
	}
	if n.leaf != nil {
		nn.leaf = CopyLeaf(n.leaf)
	}
	if len(n.edges) != 0 {
		nn.edges = make([]edge, len(n.edges))
		for idx, edge := range n.edges {
			nn.edges[idx].label = edge.label
			nn.edges[idx].node = CopyNode(edge.node)
		}
	}
	return nn
}

func CopyLeaf(l *leafNode) *leafNode {
	ll := &leafNode{
		key:      l.key,
		val:      l.val,
	}
	return ll
}

func TestRadix(t *testing.T) {
	var min, max string
	inp := make(map[string]interface{})
	for i := 0; i < 1000; i++ {
		gen, err := uuid.GenerateUUID()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		inp[gen] = i
		if gen < min || i == 0 {
			min = gen
		}
		if gen > max || i == 0 {
			max = gen
		}
	}

	r := New()
	rCopy := CopyTree(r)
	for k, v := range inp {
		newR, _, _ := r.Insert([]byte(k), v)
		if !reflect.DeepEqual(r, rCopy) {
			t.Errorf("r: %#v rc: %#v", r, rCopy)
			t.Errorf("r: %#v rc: %#v", r.root, rCopy.root)
		}
		r = newR
		rCopy = CopyTree(r)
	}

	for k, v := range inp {
		out, ok := r.Get([]byte(k))
		if !ok {
			t.Fatalf("missing key: %v", k)
		}
		if out != v {
			t.Fatalf("value mis-match: %v %v", out, v)
		}
	}

	// Check min and max
	outMin, _, _ := r.Root().Minimum()
	if string(outMin) != min {
		t.Fatalf("bad minimum: %v %v", outMin, min)
	}
	outMax, _, _ := r.Root().Maximum()
	if string(outMax) != max {
		t.Fatalf("bad maximum: %v %v", outMax, max)
	}

	// Copy the full tree before delete
	orig := r
	origCopy := CopyTree(r)

	for k, v := range inp {
		tree, out, ok := r.Delete([]byte(k))
		r = tree
		if !ok {
			t.Fatalf("missing key: %v", k)
		}
		if out != v {
			t.Fatalf("value mis-match: %v %v", out, v)
		}
	}

	if !reflect.DeepEqual(orig, origCopy) {
		t.Fatalf("structure modified")
	}
}

func TestRoot(t *testing.T) {
	r := New()
	r, _, ok := r.Delete(nil)
	if ok {
		t.Fatalf("bad")
	}
	r, _, ok = r.Insert(nil, true)
	if ok {
		t.Fatalf("bad")
	}
	val, ok := r.Get(nil)
	if !ok || val != true {
		t.Fatalf("bad: %#v", val)
	}
	r, val, ok = r.Delete(nil)
	if !ok || val != true {
		t.Fatalf("bad: %v", val)
	}
}

func TestInsert_UpdateFeedback(t *testing.T) {
	r := New()
	txn1 := r.Txn()

	for i := 0; i < 10; i++ {
		var old interface{}
		var didUpdate bool
		old, didUpdate = txn1.Insert([]byte("helloworld"), i)
		if i == 0 {
			if old != nil || didUpdate {
				t.Fatalf("bad: %d %v %v", i, old, didUpdate)
			}
		} else {
			if old == nil || old.(int) != i-1 || !didUpdate {
				t.Fatalf("bad: %d %v %v", i, old, didUpdate)
			}
		}
	}
}

func TestDelete(t *testing.T) {
	r := New()
	s := []string{"", "A", "AB"}

	for _, ss := range s {
		r, _, _ = r.Insert([]byte(ss), true)
	}
	var ok bool
	for _, ss := range s {
		r, _, ok = r.Delete([]byte(ss))
		if !ok {
			t.Fatalf("bad %q", ss)
		}
	}
}

func TestDeletePrefix(t *testing.T) {

	type exp struct {
		desc        string
		treeNodes   []string
		prefix      string
		expectedOut []string
	}

	//various test cases where DeletePrefix should succeed
	cases := []exp{
		{
			"prefix not a node in tree",
			[]string{
				"",
				"test/test1",
				"test/test2",
				"test/test3",
				"R",
				"RA"},
			"test",
			[]string{
				"",
				"R",
				"RA",
			},
		},
		{
			"prefix matches a node in tree",
			[]string{
				"",
				"test",
				"test/test1",
				"test/test2",
				"test/test3",
				"test/testAAA",
				"R",
				"RA",
			},
			"test",
			[]string{
				"",
				"R",
				"RA",
			},
		},
		{
			"longer prefix, but prefix is not a node in tree",
			[]string{
				"",
				"test/test1",
				"test/test2",
				"test/test3",
				"test/testAAA",
				"R",
				"RA",
			},
			"test/test",
			[]string{
				"",
				"R",
				"RA",
			},
		},
		{
			"prefix only matches one node",
			[]string{
				"",
				"AB",
				"ABC",
				"AR",
				"R",
				"RA",
			},
			"AR",
			[]string{
				"",
				"AB",
				"ABC",
				"R",
				"RA",
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.desc, func(t *testing.T) {
			r := New()
			for _, ss := range testCase.treeNodes {
				r, _, _ = r.Insert([]byte(ss), true)
			}
			r, ok := r.DeletePrefix([]byte(testCase.prefix))
			if !ok {
				t.Fatalf("DeletePrefix should have returned true for tree %v, deleting prefix %v", testCase.treeNodes, testCase.prefix)
			}

			verifyTree(t, testCase.expectedOut, r)
			//Delete a non-existant node
			r, ok = r.DeletePrefix([]byte("CCCCC"))
			if ok {
				t.Fatalf("Expected DeletePrefix to return false ")
			}
			verifyTree(t, testCase.expectedOut, r)
		})
	}
}

func verifyTree(t *testing.T, expected []string, r *Tree) {
	root := r.Root()
	out := []string{}
	fn := func(k []byte, v interface{}) bool {
		out = append(out, string(k))
		return false
	}
	root.Walk(fn)

	if !reflect.DeepEqual(expected, out) {
		t.Fatalf("Unexpected contents of tree after delete prefix: expected %v, but got %v", expected, out)
	}
}

func TestLongestPrefix(t *testing.T) {
	r := New()

	keys := []string{
		"",
		"foo",
		"foobar",
		"foobarbaz",
		"foobarbazzip",
		"foozip",
	}
	for _, k := range keys {
		r, _, _ = r.Insert([]byte(k), nil)
	}

	type exp struct {
		inp string
		out string
	}
	cases := []exp{
		{"a", ""},
		{"abc", ""},
		{"fo", ""},
		{"foo", "foo"},
		{"foob", "foo"},
		{"foobar", "foobar"},
		{"foobarba", "foobar"},
		{"foobarbaz", "foobarbaz"},
		{"foobarbazzi", "foobarbaz"},
		{"foobarbazzip", "foobarbazzip"},
		{"foozi", "foo"},
		{"foozip", "foozip"},
		{"foozipzap", "foozip"},
	}
	root := r.Root()
	for _, test := range cases {
		m, _, ok := root.LongestPrefix([]byte(test.inp))
		if !ok {
			t.Fatalf("no match: %v", test)
		}
		if string(m) != test.out {
			t.Fatalf("mis-match: %v %v", m, test)
		}
	}
}

func TestWalkPrefix(t *testing.T) {
	r := New()

	keys := []string{
		"foobar",
		"foo/bar/baz",
		"foo/baz/bar",
		"foo/zip/zap",
		"zipzap",
	}
	for _, k := range keys {
		r, _, _ = r.Insert([]byte(k), nil)
	}

	type exp struct {
		inp string
		out []string
	}
	cases := []exp{
		{
			"f",
			[]string{"foobar", "foo/bar/baz", "foo/baz/bar", "foo/zip/zap"},
		},
		{
			"foo",
			[]string{"foobar", "foo/bar/baz", "foo/baz/bar", "foo/zip/zap"},
		},
		{
			"foob",
			[]string{"foobar"},
		},
		{
			"foo/",
			[]string{"foo/bar/baz", "foo/baz/bar", "foo/zip/zap"},
		},
		{
			"foo/b",
			[]string{"foo/bar/baz", "foo/baz/bar"},
		},
		{
			"foo/ba",
			[]string{"foo/bar/baz", "foo/baz/bar"},
		},
		{
			"foo/bar",
			[]string{"foo/bar/baz"},
		},
		{
			"foo/bar/baz",
			[]string{"foo/bar/baz"},
		},
		{
			"foo/bar/bazoo",
			[]string{},
		},
		{
			"z",
			[]string{"zipzap"},
		},
	}

	root := r.Root()
	for _, test := range cases {
		out := []string{}
		fn := func(k []byte, v interface{}) bool {
			out = append(out, string(k))
			return false
		}
		root.WalkPrefix([]byte(test.inp), fn)
		sort.Strings(out)
		sort.Strings(test.out)
		if !reflect.DeepEqual(out, test.out) {
			t.Fatalf("mis-match: %v %v", out, test.out)
		}
	}
}

func TestWalkPath(t *testing.T) {
	r := New()

	keys := []string{
		"foo",
		"foo/bar",
		"foo/bar/baz",
		"foo/baz/bar",
		"foo/zip/zap",
		"zipzap",
	}
	for _, k := range keys {
		r, _, _ = r.Insert([]byte(k), nil)
	}

	type exp struct {
		inp string
		out []string
	}
	cases := []exp{
		{
			"f",
			[]string{},
		},
		{
			"foo",
			[]string{"foo"},
		},
		{
			"foo/",
			[]string{"foo"},
		},
		{
			"foo/ba",
			[]string{"foo"},
		},
		{
			"foo/bar",
			[]string{"foo", "foo/bar"},
		},
		{
			"foo/bar/baz",
			[]string{"foo", "foo/bar", "foo/bar/baz"},
		},
		{
			"foo/bar/bazoo",
			[]string{"foo", "foo/bar", "foo/bar/baz"},
		},
		{
			"z",
			[]string{},
		},
	}

	root := r.Root()
	for _, test := range cases {
		out := []string{}
		fn := func(k []byte, v interface{}) bool {
			out = append(out, string(k))
			return false
		}
		root.WalkPath([]byte(test.inp), fn)
		sort.Strings(out)
		sort.Strings(test.out)
		if !reflect.DeepEqual(out, test.out) {
			t.Fatalf("mis-match: %v %v", out, test.out)
		}
	}
}

func TestIteratePrefix(t *testing.T) {
	r := New()

	keys := []string{
		"foo/bar/baz",
		"foo/baz/bar",
		"foo/zip/zap",
		"foobar",
		"zipzap",
	}
	for _, k := range keys {
		r, _, _ = r.Insert([]byte(k), nil)
	}

	type exp struct {
		inp string
		out []string
	}
	cases := []exp{
		{
			"",
			keys,
		},
		{
			"f",
			[]string{
				"foo/bar/baz",
				"foo/baz/bar",
				"foo/zip/zap",
				"foobar",
			},
		},
		{
			"foo",
			[]string{
				"foo/bar/baz",
				"foo/baz/bar",
				"foo/zip/zap",
				"foobar",
			},
		},
		{
			"foob",
			[]string{"foobar"},
		},
		{
			"foo/",
			[]string{"foo/bar/baz", "foo/baz/bar", "foo/zip/zap"},
		},
		{
			"foo/b",
			[]string{"foo/bar/baz", "foo/baz/bar"},
		},
		{
			"foo/ba",
			[]string{"foo/bar/baz", "foo/baz/bar"},
		},
		{
			"foo/bar",
			[]string{"foo/bar/baz"},
		},
		{
			"foo/bar/baz",
			[]string{"foo/bar/baz"},
		},
		{
			"foo/bar/bazoo",
			[]string{},
		},
		{
			"z",
			[]string{"zipzap"},
		},
	}

	root := r.Root()
	for idx, test := range cases {
		iter := root.Iterator()
		if test.inp != "" {
			iter.SeekPrefix([]byte(test.inp))
		}

		// Consume all the keys
		out := []string{}
		for {
			key, _, ok := iter.Next()
			if !ok {
				break
			}
			out = append(out, string(key))
		}
		if !reflect.DeepEqual(out, test.out) {
			t.Fatalf("mis-match: %d %v %v", idx, out, test.out)
		}
	}
}

func TestMergeChildNilEdges(t *testing.T) {
	r := New()
	r, _, _ = r.Insert([]byte("foobar"), 42)
	r, _, _ = r.Insert([]byte("foozip"), 43)
	r, _, _ = r.Delete([]byte("foobar"))

	root := r.Root()
	out := []string{}
	fn := func(k []byte, v interface{}) bool {
		out = append(out, string(k))
		return false
	}
	root.Walk(fn)

	expect := []string{"foozip"}
	sort.Strings(out)
	sort.Strings(expect)
	if !reflect.DeepEqual(out, expect) {
		t.Fatalf("mis-match: %v %v", out, expect)
	}
}

func TestMergeChildVisibility(t *testing.T) {
	r := New()
	r, _, _ = r.Insert([]byte("foobar"), 42)
	r, _, _ = r.Insert([]byte("foobaz"), 43)
	r, _, _ = r.Insert([]byte("foozip"), 10)

	txn1 := r.Txn()
	txn2 := r.Txn()

	// Ensure we get the expected value foobar and foobaz
	if val, ok := txn1.Get([]byte("foobar")); !ok || val != 42 {
		t.Fatalf("bad: %v", val)
	}
	if val, ok := txn1.Get([]byte("foobaz")); !ok || val != 43 {
		t.Fatalf("bad: %v", val)
	}
	if val, ok := txn2.Get([]byte("foobar")); !ok || val != 42 {
		t.Fatalf("bad: %v", val)
	}
	if val, ok := txn2.Get([]byte("foobaz")); !ok || val != 43 {
		t.Fatalf("bad: %v", val)
	}

	// Delete of foozip will cause a merge child between the
	// "foo" and "ba" nodes.
	if val, ok := txn2.Delete([]byte("foozip")); !ok || val != 10 {
		t.Fatalf("bad: %v", val)
	}

	// Insert of "foobaz" will update the slice of the "fooba" node
	// in-place to point to the new "foobaz" node. This in-place update
	// will cause the visibility of the update to leak into txn1 (prior
	// to the fix).
	if val, ok := txn2.Insert([]byte("foobaz"), 44); !ok || val != 43 {
		t.Fatalf("bad: %v", val)
	}

	// Ensure we get the expected value foobar and foobaz
	if val, ok := txn1.Get([]byte("foobar")); !ok || val != 42 {
		t.Fatalf("bad: %v", val)
	}
	if val, ok := txn1.Get([]byte("foobaz")); !ok || val != 43 {
		t.Fatalf("bad: %v", val)
	}
	if val, ok := txn2.Get([]byte("foobar")); !ok || val != 42 {
		t.Fatalf("bad: %v", val)
	}
	if val, ok := txn2.Get([]byte("foobaz")); !ok || val != 44 {
		t.Fatalf("bad: %v", val)
	}

	// Commit txn2
	r, _ = txn2.Commit()

	// Ensure we get the expected value foobar and foobaz
	if val, ok := txn1.Get([]byte("foobar")); !ok || val != 42 {
		t.Fatalf("bad: %v", val)
	}
	if val, ok := txn1.Get([]byte("foobaz")); !ok || val != 43 {
		t.Fatalf("bad: %v", val)
	}
	if val, ok := r.Get([]byte("foobar")); !ok || val != 42 {
		t.Fatalf("bad: %v", val)
	}
	if val, ok := r.Get([]byte("foobaz")); !ok || val != 44 {
		t.Fatalf("bad: %v", val)
	}
}

func TestLenTxn(t *testing.T) {
	r := New()
	txn := r.Txn()
	keys := []string{
		"foo/bar/baz",
		"foo/baz/bar",
		"foo/zip/zap",
		"foobar",
		"nochange",
	}
	for _, k := range keys {
		txn.Insert([]byte(k), nil)
	}
	r, _ = txn.Commit()


	txn = r.Txn()
	for _, k := range keys {
		txn.Delete([]byte(k))
	}
	r, _ = txn.Commit()
}

func TestIterateLowerBound(t *testing.T) {
	fixedLenKeys := []string{
		"00000",
		"00001",
		"00004",
		"00010",
		"00020",
		"20020",
	}

	mixedLenKeys := []string{
		"a1",
		"abc",
		"barbazboo",
		"foo",
		"found",
		"zap",
		"zip",
	}

	type exp struct {
		keys   []string
		search string
		want   []string
	}
	cases := []exp{
		{
			fixedLenKeys,
			"00000",
			fixedLenKeys,
		}, {
			fixedLenKeys,
			"00003",
			[]string{
				"00004",
				"00010",
				"00020",
				"20020",
			},
		}, {
			fixedLenKeys,
			"00010",
			[]string{
				"00010",
				"00020",
				"20020",
			},
		}, {
			fixedLenKeys,
			"20000",
			[]string{
				"20020",
			},
		}, {
			fixedLenKeys,
			"20020",
			[]string{
				"20020",
			},
		}, {
			fixedLenKeys,
			"20022",
			[]string{},
		}, {
			mixedLenKeys,
			"A", // before all lower case letters
			mixedLenKeys,
		}, {
			mixedLenKeys,
			"a1",
			mixedLenKeys,
		}, {
			mixedLenKeys,
			"b",
			[]string{
				"barbazboo",
				"foo",
				"found",
				"zap",
				"zip",
			},
		}, {
			mixedLenKeys,
			"bar",
			[]string{
				"barbazboo",
				"foo",
				"found",
				"zap",
				"zip",
			},
		}, {
			mixedLenKeys,
			"barbazboo0",
			[]string{
				"foo",
				"found",
				"zap",
				"zip",
			},
		}, {
			mixedLenKeys,
			"zippy",
			[]string{},
		}, {
			mixedLenKeys,
			"zi",
			[]string{
				"zip",
			},
		},

		// This is a case found by TestIterateLowerBoundFuzz simplified by hand. The
		// lowest node should be the first, but it is split on the same char as the
		// second char in the search string. My initial implementation didn't take
		// that into account (i.e. propagate the fact that we already know we are
		// greater than the input key into the recursion). This would skip the first
		// result.
		{
			[]string{
				"bb",
				"bc",
			},
			"ac",
			[]string{"bb", "bc"},
		},

		// This is a case found by TestIterateLowerBoundFuzz.
		{
			[]string{"aaaba", "aabaa", "aabab", "aabcb", "aacca", "abaaa", "abacb", "abbcb", "abcaa", "abcba", "abcbb", "acaaa", "acaab", "acaac", "acaca", "acacb", "acbaa", "acbbb", "acbcc", "accca", "babaa", "babcc", "bbaaa", "bbacc", "bbbab", "bbbac", "bbbcc", "bbcab", "bbcca", "bbccc", "bcaac", "bcbca", "bcbcc", "bccac", "bccbc", "bccca", "caaab", "caacc", "cabac", "cabbb", "cabbc", "cabcb", "cacac", "cacbc", "cacca", "cbaba", "cbabb", "cbabc", "cbbaa", "cbbab", "cbbbc", "cbcbb", "cbcbc", "cbcca", "ccaaa", "ccabc", "ccaca", "ccacc", "ccbac", "cccaa", "cccac", "cccca"},
			"cbacb",
			[]string{"cbbaa", "cbbab", "cbbbc", "cbcbb", "cbcbc", "cbcca", "ccaaa", "ccabc", "ccaca", "ccacc", "ccbac", "cccaa", "cccac", "cccca"},
		},
	}

	for idx, test := range cases {
		t.Run(fmt.Sprintf("case%03d", idx), func(t *testing.T) {
			r := New()

			// Insert keys
			for _, k := range test.keys {
				var ok bool
				r, _, ok = r.Insert([]byte(k), nil)
				if ok {
					t.Fatalf("duplicate key %s in keys", k)
				}
			}
			// Get and seek iterator
			root := r.Root()
			iter := root.Iterator()
			if test.search != "" {
				iter.SeekLowerBound([]byte(test.search))
			}

			// Consume all the keys
			out := []string{}
			for {
				key, _, ok := iter.Next()
				if !ok {
					break
				}
				out = append(out, string(key))
			}
			if !reflect.DeepEqual(out, test.want) {
				t.Fatalf("mis-match: key=%s\n  got=%v\n  want=%v", test.search,
					out, test.want)
			}
		})
	}
}

type readableString string

func (s readableString) Generate(rand *rand.Rand, size int) reflect.Value {
	// Pick a random string from a limited alphabet that makes it easy to read the
	// failure cases. Also never includes a null byte as we don't support that.
	const letters = "abcdefghijklmnopqrstuvwxyz/-_0123456789"

	b := make([]byte, size)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return reflect.ValueOf(readableString(b))
}

func TestIterateLowerBoundFuzz(t *testing.T) {
	r := New()
	set := []string{}

	// This specifies a property where each call adds a new random key to the radix
	// tree (with a null byte appended since our tree doesn't support one key
	// being a prefix of another and treats null bytes specially).
	//
	// It also maintains a plain sorted list of the same set of keys and asserts
	// that iterating from some random key to the end using LowerBound produces
	// the same list as filtering all sorted keys that are lower.

	radixAddAndScan := func(newKey, searchKey readableString) []string {
		// Append a null byte
		key := []byte(newKey + "\x00")
		r, _, _ = r.Insert(key, nil)

		// Now iterate the tree from searchKey to the end
		it := r.Root().Iterator()
		result := []string{}
		it.SeekLowerBound([]byte(searchKey))
		for {
			key, _, ok := it.Next()
			if !ok {
				break
			}
			// Strip the null byte and append to result set
			result = append(result, string(key[0:len(key)-1]))
		}
		return result
	}

	sliceAddSortAndFilter := func(newKey, searchKey readableString) []string {
		// Append the key to the set and re-sort
		set = append(set, string(newKey))
		sort.Strings(set)

		t.Logf("Current Set: %#v", set)

		result := []string{}
		var prev string
		for _, k := range set {
			if k >= string(searchKey) && k != prev {
				result = append(result, k)
			}
			prev = k
		}
		return result
	}

	if err := quick.CheckEqual(radixAddAndScan, sliceAddSortAndFilter, nil); err != nil {
		t.Error(err)
	}
}

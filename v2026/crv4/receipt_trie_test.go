// Independent Rust sp-trie vectors are shared with the root-action receipt
// qualification; production receipt code never depends on that command package.
package crv4

import "testing"

// Generated with SDK cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a. Inputs cross
// compact-index, inline-child and hashed-value boundaries in both layouts.
func TestReceiptTrieMatchesPinnedRust(t *testing.T) {
	vectors := []struct {
		count int
		roots [2]string
	}{
		{count: 0, roots: [2]string{"03170a2e7597b7b7e3d84c05391d139a62b157e78786d8c082f29dcf4c111314", "03170a2e7597b7b7e3d84c05391d139a62b157e78786d8c082f29dcf4c111314"}},
		{count: 1, roots: [2]string{"27ca70a13c772e41e0f81ec9353f1b4f6a2b82b08d87a57e97e40036d16d997b", "27ca70a13c772e41e0f81ec9353f1b4f6a2b82b08d87a57e97e40036d16d997b"}},
		{count: 2, roots: [2]string{"04e3df244bca6dc77717cb84a3d91012dfc7f574993ce83ba128be6944d660d3", "04e3df244bca6dc77717cb84a3d91012dfc7f574993ce83ba128be6944d660d3"}},
		{count: 16, roots: [2]string{"075f18142c9494d8104529e0f9bf262a178b5ca0fa9506a774847a2bf5cfd10b", "075f18142c9494d8104529e0f9bf262a178b5ca0fa9506a774847a2bf5cfd10b"}},
		{count: 64, roots: [2]string{"df9b54b4042b261298d443a166fb41ab0f9426c32de85af8ccef370caf9fd4dd", "c0fc8bc715c9e59823411fbd5968f6ef51aa6bba02a480339cef92318f9d747c"}},
		{count: 65, roots: [2]string{"66079b4e40b6bb0f512c86e5e0a78b9bedfc290c0729ea471b01b4c4265ca82d", "159b2efbfeb273fd4a32de73c40cc82a8fe3f368af7e7c85aa3c781d1407efcf"}},
		{count: 256, roots: [2]string{"3a8f3fe205e0a07fe0514d56b5770035fd9218bc22c81b387616c8e6d94e3eee", "ded6205562f8f80a2eca6117d17e2aefeaf416ac0434e2b955e8697838d692d9"}},
		{count: 1024, roots: [2]string{"10763206d959aeb23bbc641f6bafca30148765ab835dae8102b77a1c2e33ed93", "4555cb3d445d8e5275c82c04b988d31538eed5a6e431b0d3b1d52ec3f21f4899"}},
	}
	for _, vector := range vectors {
		values := make([][]byte, vector.count)
		for index := range values {
			values[index] = make([]byte, index%67+1)
			for column := range values[index] {
				values[index][column] = byte(index*17 + column*31)
			}
		}
		for layout, expected := range vector.roots {
			actual, err := receiptExtrinsicsRoot(values, uint8(layout))
			if err != nil || actual.Hex() != "0x"+expected {
				t.Fatalf("Rust vector %d layout %d: %s %v", vector.count, layout, actual.Hex(), err)
			}
		}
	}
}

// Body work has independent count/byte bounds; unsupported encodings cannot
// silently become an empty known-layout root.
func TestReceiptTrieRejectsUnboundedOrUnknownBody(t *testing.T) {
	for _, test := range []struct {
		body   [][]byte
		layout uint8
	}{
		{layout: 2},
		{body: make([][]byte, receiptBodyCountLimit+1)},
		{body: [][]byte{{}}},
		{body: [][]byte{make([]byte, receiptBodyBytesLimit), {1}}},
	} {
		if _, err := receiptExtrinsicsRoot(test.body, test.layout); err == nil {
			t.Fatal("unbounded or unsupported receipt body was accepted")
		}
	}
}

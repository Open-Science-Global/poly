package poly

import (
	"fmt"
	"testing"
)

func ExampleLinearFold() {

	result, score := LinearFold("UGAGUUCUCGAUCUCUAAAAUCG")

	fmt.Printf("result: %v , score: %v", result, score)
	// Output: result: ....................... , score: -0.22376699999999988
}


func TestLinearFold(t *testing.T) {
	var result string
	var score float64

	result, score = LinearFold("UGAGUUCUCGAUCUCUAAAAUCG")
	if fmt.Sprintf("result: %v , score: %v", result, score) != "result: ....................... , score: -0.22376699999999988" {
		t.Errorf("Failed to fold UGAGUUCUCGAUCUCUAAAAUCG. Expected 'result: ....................... , score: -0.22376699999999988', got " + fmt.Sprintf("result: %v , score: %v", result, score))
	}
	result, score = LinearFold("AAAACGGUCCUUAUCAGGACCAAACA")
	if fmt.Sprintf("result: %v , score: %v", result, score) != "result: .....((((((....))))))..... , score: 4.90842861201" {
		t.Errorf("Failed to fold AAAACGGUCCUUAUCAGGACCAAACA. Expected 'result: .....((((((....))))))..... , score: 4.90842861201', got " + fmt.Sprintf("result: %v , score: %v", result, score))
	}
	result, score = LinearFold("AUUCUUGCUUCAACAGUGUUUGAACGGAAU")
	if fmt.Sprintf("result: %v , score: %v", result, score) != "result: .............................. , score: -0.2918699999999998" {
		t.Errorf("Failed to fold AUUCUUGCUUCAACAGUGUUUGAACGGAAU. Expected 'result: .............................. , score: -0.2918699999999998', got " + fmt.Sprintf("result: %v , score: %v", result, score))
	}
	result, score = LinearFold("UCGGCCACAAACACACAAUCUACUGUUGGUCGA")
	if fmt.Sprintf("result: %v , score: %v", result, score) != "result: (((((((...................))))))) , score: 0.9879274133299999" {
		t.Errorf("Failed to fold UCGGCCACAAACACACAAUCUACUGUUGGUCGA. Expected 'result: (((((((...................))))))) , score: 0.9879274133299999', got " + fmt.Sprintf("result: %v , score: %v", result, score))
	}
	result, score = LinearFold("GUUUUUAUCUUACACACGCUUGUGUAAGAUAGUUA")
	if fmt.Sprintf("result: %v , score: %v", result, score) != "result: .....(((((((((((....))))))))))).... , score: 6.660038360289999" {
		t.Errorf("Failed to fold GUUUUUAUCUUACACACGCUUGUGUAAGAUAGUUA. Expected 'result: .....(((((((((((....))))))))))).... , score: 6.660038360289999', got " + fmt.Sprintf("result: %v , score: %v", result, score))
	}

	// In order to run these, we need to set the beam size
	beam_size = 20
	result, score = LinearFold("GGGCUCGUAGAUCAGCGGUAGAUCGCUUCCUUCGCAAGGAAGCCCUGGGUUCAAAUCCCAGCGAGUCCACCA")
	if fmt.Sprintf("result: %v , score: %v", result, score) != "result: (((((((..((((.......))))(((((((.....))))))).(((((.......)))))))))))).... , score: 13.974768784808" {
		t.Errorf("Failed to fold GGGCUCGUAGAUCAGCGGUAGAUCGCUUCCUUCGCAAGGAAGCCCUGGGUUCAAAUCCCAGCGAGUCCACCA. Expected 'result: (((((((..((((.......))))(((((((.....))))))).(((((.......)))))))))))).... , score: 13.974768784808', got " + fmt.Sprintf("result: %v , score: %v", result, score))
	}
}

func BenchmarkLinearfold(b *testing.B) {
	// Run linearfold on a 2915bp sequence
	for n := 0; n < b.N; n++ {
		LinearFold("GGUCAAGAUGGUAAGGGCCCACGGUGGAUGCCUCGGCACCCGAGCCGAUGAAGGACGUGGCUACCUGCGAUAAGCCAGGGGGAGCCGGUAGCGGGCGUGGAUCCCUGGAUGUCCGAAUGGGGGAACCCGGCCGGCGGGAACGCCGGUCACCGCGCUUUUGCGCGGGGGGAACCUGGGGAACUGAAACAUCUCAGUACCCAGAGGAGAGGAAAGAGAAAUCGACUCCCUGAGUAGCGGCGAGCGAAAGGGGACCAGCCUAAACCGUCCGGCUUGUCCGGGCGGGGUCGUGGGGCCCUCGGACACCGAAUCCCCAGCCUAGCCGAAGCUGUUGGGAAGCAGCGCCAGAGAGGGUGAAAGCCCCGUAGGCGAAAGGUGGGGGGAUAGGUGAGGGUACCCGAGUACCCCGUGGUUCGUGGAGCCAUGGGGGAAUCUGGGCGGACCACCGGCCUAAGGCUAAGUACUCCGGGUGACCGAUAGCGCACCAGUACCGUGAGGGAAAGGUGAAAAGAACCCCGGGAGGGAGUGAAAUAGAGCCUGAAACCGUGGGCUUACAAGCAGUCACGGCCCCGCAAGGGGUUGUGGCGUGCCUAUUGAAGCAUGAGCCGGCGACUCACGGUCGUGGGCGAGCUUAAGCCGUUGAGGCGGAGGCGUAGGGAAACCGAGUCCGAACAGGGCGCAAGCGGGCCGCACGCGGCCCGCAAAGUCCGCGGCCGUGGACCCGAAACCGGGCGAGCUAGCCCUGGCCAGGGUGAAGCUGGGGUGAGACCCAGUGGAGGCCCGAACCGGUGGGGGAUGCAAACCCCUCGGAUGAGCUGGGGCUAGGAGUGAAAAGCUAACCGAGCCCGGAGAUAGCUGGUUCUCCCCGAAAUGACUUUAGGGUCAGCCUCAGGCGCUGACUGGGGCCUGUAGAGCACUGAUAGGGCUAGGGGGCCCACCAGCCUACCAAACCCUGUCAAACUCCGAAGGGUCCCAGGUGGAGCCUGGGAGUGAGGGCGCGAGCGAUAACGUCCGCGUCCGAGCGCGGGAACAACCGAGACCGCCAGCUAAGGCCCCCAAGUCUGGGCUAAGUGGUAAAGGAUGUGGCGCCGCGAAGACAGCCAGGAGGUUGGCUUAGAAGCAGCCAUCCUUUAAAGAGUGCGUAAUAGCUCACUGGUCGAGUGGCGCCGCGCCGAAAAUGAUGCGGGGCUUAAGCCCAGCGCCGAAGCUGCGGGUCUGGGGGAUGACCCCAGGCGGUAGGGGAGCGUUCCCGAUGCCGAUGAAGGCCGACCCGCGAGGCGGCUGGAGGUAAGGGAAGUGCGAAUGCCGGCAUGAGUAACGAUAAAGAGGGUGAGAAUCCCUCUCGCCGUAAGCCCAAGGGUUCCUACGCAAUGGUCGUCAGCGUAGGGUUAGGCGGGACCUAAGGUGAAGCCGAAAGGCGUAGCCGAAGGGCAGCCGGUUAAUAUUCCGGCCCUUCCCGCAGGUGCGAUGGGGGGACGCUCUAGGCUAGGGGGACCGGAGCCAUGGACGAGCCCGGCCAGAAGCGCAGGGUGGGAGGUAGGCAAAUCCGCCUCCCAACAAGCUCUGCGUGGUGGGGAAGCCCGUACGGGUGACAACCCCCCGAAGCCAGGGAGCCAAGAAAAGCCUCUAAGCACAACCUGCGGGAACCCGUACCGCAAACCGACACAGGUGGGCGGGUGCAAGAGCACUCAGGCGCGCGGGAGAACCCUCGCCAAGGAACUCUGCAAGUUGGCCCCGUAACUUCGGGAGAAGGGGUGCUCCCUGGGGUGAUGAGCCCCGGGGAGCCGCAGUGAACAGGCUCUGGCGACUGUUUACCAAAAACACAGCUCUCUGCGAACUCGUAAGAGGAGGUAUAGGGAGCGACGCUUGCCCGGUGCCGGAAGGUCAAGGGGAGGGGUGCAAGCCCCGAACCGAAGCCCCGGUGAACGGCGGCCGUAACUAUAACGGUCCUAAGGUAGCGAAAUUCCUUGUCGGGUAAGUUCCGACCUGCACGAAAAGCGUAACGACCGGAGCGCUGUCUCGGCGAGGGACCCGGUGAAAUUGAACUGGCCGUGAAGAUGCGGCCUACCCGUGGCAGGACGAAAAGACCCCGUGGAGCUUUACUGCAGCCUGGUGUUGGCUCUUGGUCGCGCCUGCGUAGGAUAGGUGGGAGCCUGUGAACCCCCGCCUCCGGGUGGGGGGGAGGCGCCGGUGAAAUACCACCCUGGCGCGGCUGGGGGCCUAACCCUCGGAUGGGGGGACAGCGCUUGGCGGGCAGUUUGACUGGGGCGGUCGCCUCCUAAAAGGUAACGGAGGCGCCCAAAGGUCCCCUCAGGCGGGACGGAAAUCCGCCGGAGAGCGCAAGGGUAGAAGGGGGCCUGACUGCGAGGCCUGCAAGCCGAGCAGGGGCGAAAGCCGGGCCUAGUGAACCGGUGGUCCCGUGUGGAAGGGCCAUCGAUCAACGGAUAAAAGUUACCCCGGGGAUAACAGGCUGAUCUCCCCCGAGCGUCCACAGCGGCGGGGAGGUUUGGCACCUCGAUGUCGGCUCGUCGCAUCCUGGGGCUGAAGAAGGUCCCAAGGGUUGGGCUGUUCGCCCAUUAAAGCGGCACGCGAGCUGGGUUCAGAACGUCGUGAGACAGUUCGGUCUCUAUCCGCCACGGGCGCAGGAGGCUUGAGGGGGGCUCUUCCUAGUACGAGAGGACCGGAAGGGACGCACCUCUGGUUUCCCAGCUGUCCCUCCAGGGGCAUAAGCUGGGUAGCCAUGUGCGGAAGGGAUAACCGCUGAAAGCAUCUAAGCGGGAAGCCCGCCCCAAGAUGAGGCCUCCCACGGCGUCAAGCCGGUAAGGACCCGGGAAGACCACCCGGUGGAUGGGCCGGGGGUGUAAGCGCCGCGAGGCGUUGAGCCGACCGGUCCCAAUCGUCCGAGGUCUUGACCCCUC")
	}
}

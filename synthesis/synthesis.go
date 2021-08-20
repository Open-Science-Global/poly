package synthesis

import (
	"errors"
	"regexp"
	"strings"
	"sync"

	"github.com/Open-Science-Global/poly/checks"
	"github.com/Open-Science-Global/poly/linearfold"
	"github.com/Open-Science-Global/poly/mfe"
	"github.com/Open-Science-Global/poly/secondary_structure"
	"github.com/Open-Science-Global/poly/transform"
	"github.com/Open-Science-Global/poly/transform/codon"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

/******************************************************************************
Apr 25, 2021

DNA synthesis fixing stuff starts here.

[Check commit history for previous comments]

The primary goal of the synthesis fixer is to fix protein coding sequences (CDS) in preparation
for DNA synthesis. Because CDSs are flexible in their coding, we can change codons to remove
problematic parts of sequences. The synthesis fixer is built to be very fast while covering the
majority of use-cases for synthesis fixing.

Our synthesis fixer function (FixCds) takes in 3 parameters: a coding sequence, a codon table,
and a list of functions that identify problematic sequences. These functions output not only
the region where a problem occurs, but also some heuristics to help the function quickly find a
solution to the given constraints. For example, function outputs can include a gc bias.

The synthesis fixer works in a declarative rather than imperative approach. We spend a lot of time
setting up our problem space so that we can quickly and easily move to a solution. First, we build
an in-memory SQLite database with everything about our sequence - including its codon table,
possible codon-to-codont transformations, and the sequence itself.

We then run all the user-provided problematicSequenceFuncs in-parallel to generate DnaSuggestions
of places that we should fix in our sequence. Then, we check if there are any DnaSuggestions that
overlap - for example, perhaps a repeat region needs to be fixed in a location with a high GC content,
so we bias fixing that repeat region to fixes that have a high AT content. If there are any overlapping
suggestions, those suggestions are fixed first.

Once we have a list of those suggestions, we fix the sequence using some simple SQL. Once our first round
of fixes is applied, we test the sequence again to see if we missed any problematic sequence locations. We
do that for 100 rounds, at which point we just give up and return the sequence to the user as is.

Hopefully, this will enable more high throughput synthesis of genes!

Keoni

[started Dec 9, 2020]
******************************************************************************/

var FixIterations = 100

// DnaSuggestion is a suggestion of a fixer, generated by a problematicSequenceFunc.
type DnaSuggestion struct {
	Start          int    `db:"start"`
	End            int    `db:"end"`
	Bias           string `db:"gcbias"`
	QuantityFixes  int    `db:"quantityfixes"`
	SuggestionType string `db:"suggestiontype"`
}

type Change struct {
	Position int    `db:"position"`
	Step     int    `db:"step"`
	From     string `db:"codonfrom"`
	To       string `db:"codonto"`
	Reason   string `db:"reason"`
}

type dbDnaSuggestion struct {
	Start          int    `db:"start"`
	End            int    `db:"end"`
	Bias           string `db:"gcbias"`
	QuantityFixes  int    `db:"quantityfixes"`
	SuggestionType string `db:"suggestiontype"`
	Step           int    `db:"step"`
	ID             int    `db:"id"`
}

// RemoveSequence is a generator for a problematicSequenceFuncs for specific sequences.
func RemoveSequence(sequencesToRemove []string) func(string, chan DnaSuggestion, *sync.WaitGroup) {
	return func(sequence string, c chan DnaSuggestion, wg *sync.WaitGroup) {
		var enzymes []string
		for _, enzyme := range sequencesToRemove {
			enzymes = []string{enzyme, transform.ReverseComplement(enzyme)}
			for _, site := range enzymes {
				re := regexp.MustCompile(site)
				locs := re.FindAllStringIndex(sequence, -1)
				for _, loc := range locs {
					position := loc[0] / 3
					leftover := loc[0] % 3
					switch {
					case leftover == 0:
						c <- DnaSuggestion{position, (loc[1] / 3), "NA", 1, "Remove sequence"}
					case leftover != 0:
						c <- DnaSuggestion{position, (loc[1] / 3) - 1, "NA", 1, "Remove sequence"}
					}
				}
			}
		}
		wg.Done()
	}
}

// RemoveRepeat is a generator to make a problematicSequenceFunc for repeats.
func RemoveRepeat(repeatLen int) func(string, chan DnaSuggestion, *sync.WaitGroup) {
	return func(sequence string, c chan DnaSuggestion, wg *sync.WaitGroup) {
		// Get a kmer list
		kmers := make(map[string]bool)
		for i := 0; i < len(sequence)-repeatLen; i++ {
			_, alreadyFound := kmers[sequence[i:i+repeatLen]]
			if alreadyFound {
				position := i / 3
				leftover := i % 3
				switch {
				case leftover == 0:
					c <- DnaSuggestion{position, ((i + repeatLen) / 3), "NA", 1, "Remove repeat"}
				case leftover != 0:
					c <- DnaSuggestion{position, ((i + repeatLen) / 3) - 1, "NA", 1, "Remove repeat"}
				}
			}
			kmers[sequence[i:i+repeatLen]] = true
		}
		wg.Done()
	}
}

// GcContentFixer is a generator to increase or decrease the overall GcContent
// of a CDS.
func GcContentFixer(upperBound, lowerBound float64) func(string, chan DnaSuggestion, *sync.WaitGroup) {
	return func(sequence string, c chan DnaSuggestion, wg *sync.WaitGroup) {
		gcContent := checks.GcContent(sequence)
		var numberOfChanges int
		if gcContent > upperBound {
			numberOfChanges = int((gcContent - upperBound) * float64(len(sequence)))
			c <- DnaSuggestion{0, len(sequence), "AT", numberOfChanges, "GcContent too high"}
		}
		if gcContent < lowerBound {
			numberOfChanges = int((lowerBound - gcContent) * float64(len(sequence)))
			c <- DnaSuggestion{0, len(sequence), "GC", numberOfChanges, "GcContent too low"}
		}
		wg.Done()
	}

}

// RemoveRepeat is a generator to make a problematicSequenceFunc for repeats.
func RemoveSecondaryStructure(closeIndex int) func(string, chan DnaSuggestion, *sync.WaitGroup) {
	return func(sequence string, c chan DnaSuggestion, wg *sync.WaitGroup) {
		// Get a kmer list
		sequence = sequence[:closeIndex]

		transcripted := transform.Transcription(sequence)
		dot_bracket, _ := linearfold.CONTRAfoldV2(transcripted, linearfold.DefaultBeamSize)
		_, secondaryStructure, err := mfe.MinimumFreeEnergy(transcripted, dot_bracket, mfe.DefaultTemperature, linearfold.DefaultEnergyParamsSet, mfe.DefaultDanglingEndsModel)
		if err == nil {
			for _, structure := range secondaryStructure.Structures {
				switch structure := structure.(type) {
				case *secondary_structure.SingleStrandedRegion:
					// `SingleStrandedRegion`s don't contribute any energy
					continue
				case *secondary_structure.MultiLoop:
					i := structure.Stem.ClosingFivePrimeIdx
					endPosition := structure.Stem.ClosingThreePrimeIdx / 3
					position := i / 3
					leftover := i % 3
					switch {
					case leftover == 0:
						c <- DnaSuggestion{position, endPosition, "NA", 1, "Remove secondary structure"}
					case leftover != 0:
						c <- DnaSuggestion{position, endPosition - 1, "NA", 1, "Remove secondary structure"}
					}
				case *secondary_structure.Hairpin:
					i := structure.Stem.EnclosedFivePrimeIdx
					endPosition := structure.Stem.EnclosedThreePrimeIdx / 3
					position := i / 3
					leftover := i % 3
					switch {
					case leftover == 0:
						c <- DnaSuggestion{position, endPosition, "NA", 1, "Remove secondary structure"}
					case leftover != 0:
						c <- DnaSuggestion{position, endPosition - 1, "NA", 1, "Remove secondary structure"}
					}

				}
			}
		}

		wg.Done()
	}
}

func RemoveHairpin(stemSize int, hairpinWindow int) func(string, chan DnaSuggestion, *sync.WaitGroup) {
	return func(sequence string, c chan DnaSuggestion, wg *sync.WaitGroup) {
		reverse := transform.ReverseComplement(sequence)

		for i := 0; i < len(sequence)-stemSize && len(sequence)-(i+hairpinWindow) >= 0; i++ {
			word := sequence[i : i+stemSize]
			rest := reverse[len(sequence)-(i+hairpinWindow) : len(sequence)-(i+stemSize)]
			if strings.Contains(rest, word) {
				location := strings.Index(rest, word)
				position := i / 3
				leftover := i % 3
				switch {
				case leftover == 0:
					c <- DnaSuggestion{position, ((i + hairpinWindow - location - 1) / 3), "NA", 1, "Remove nearby reverse complement, possible hairpin"}
				case leftover != 0:
					c <- DnaSuggestion{position, ((i + hairpinWindow - location - 1) / 3) - 1, "NA", 1, "Remove nearby reverse complement, possible hairpin"}
				}
			}

		}

		wg.Done()
	}
}

// GlobalRemoveRepeat is a generator to make a function that searchs for external repeats (e.g genome repeats) and make a
// DnaSuggestion for codon changes.
func GlobalRemoveRepeat(repeatLen int, globalKmers map[string]bool) func(string, chan DnaSuggestion, *sync.WaitGroup) {
	return func(sequence string, c chan DnaSuggestion, wg *sync.WaitGroup) {
		for i := 0; i < len(sequence)-repeatLen; i++ {
			subsequence := sequence[i : i+repeatLen]
			reverseComplement := transform.ReverseComplement(subsequence)
			_, forwardGlobalRepeat := globalKmers[subsequence]
			_, reverseGlobalRepeat := globalKmers[reverseComplement]
			if forwardGlobalRepeat || reverseGlobalRepeat {
				position := i / 3
				leftover := i % 3
				switch {
				case leftover == 0:
					c <- DnaSuggestion{position, ((i + repeatLen) / 3), "NA", 1, "Remove repeat"}
				case leftover != 0:
					c <- DnaSuggestion{position, ((i + repeatLen) / 3) - 1, "NA", 1, "Remove repeat"}
				}
			}
		}
		wg.Done()
	}
}

// Ensure GC range is a generator to make a problematicSequenceFunc for gc content.

func findProblems(sequence string, problematicSequenceFuncs []func(string, chan DnaSuggestion, *sync.WaitGroup)) []DnaSuggestion {
	// Run functions to get suggestions
	suggestions := make(chan DnaSuggestion, 100)
	var wg sync.WaitGroup
	for _, f := range problematicSequenceFuncs {
		wg.Add(1)
		go f(sequence, suggestions, &wg)
	}
	wg.Wait()
	close(suggestions)

	var suggestionsList []DnaSuggestion
	for suggestion := range suggestions {
		suggestionsList = append(suggestionsList, suggestion)
	}
	return suggestionsList
}

// FixCds fixes a CDS given the CDS sequence, a codon table, and a list of functions to solve for.
func FixCds(sqlitePath string, sequence string, codontable codon.Table, problematicSequenceFuncs []func(string, chan DnaSuggestion, *sync.WaitGroup)) (string, []Change, error) {
	db := sqlx.MustConnect("sqlite3", sqlitePath)
	createMemoryDbSQL := `
	CREATE TABLE codon (
		codon TEXT PRIMARY KEY,
		aa TEXT
	);

	CREATE TABLE seq (
		pos INT PRIMARY KEY
	);

	CREATE TABLE history (
		pos INTEGER REFERENCES seq(pos),
		codon TEXT NOT NULL REFERENCES codon(codon),
		step INT,
		suggestedfix INT REFERENCES suggestedfix(id)
	);

	-- Weights are set on a per position basis for codon harmonization at a later point
	CREATE TABLE weights (
		pos INTEGER REFERENCES seq(pos),
		codon TEXT NOT NULL REFERENCES codon(codon),
		weight INTEGER
	);

	CREATE TABLE codonbias (
		fromcodon TEXT REFERENCES codon(codon),
		tocodon TEXT REFERENCES codon(codon),
		gcbias TEXT
	);

	CREATE TABLE suggestedfix (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		step INTEGER,
		start INTEGER REFERENCES seq(pos),
		end INTEGER REFERENCES seq(pos),
		gcbias TEXT,
		quantityfixes INTEGER,
		suggestiontype TEXT
	);
`
	db.MustExec(createMemoryDbSQL)
	// Insert codons
	weightTable := make(map[string]int)
	codonInsert := `INSERT INTO codon(codon, aa) VALUES (?, ?)`
	for _, aminoAcid := range codontable.AminoAcids {
		for _, codon := range aminoAcid.Codons {
			db.MustExec(codonInsert, codon.Triplet, aminoAcid.Letter)
			weightTable[codon.Triplet] = codon.Weight

			codonBias := strings.Count(codon.Triplet, "G") + strings.Count(codon.Triplet, "C")
			for _, toCodon := range aminoAcid.Codons {
				if codon.Triplet != toCodon.Triplet {
					toCodonBias := strings.Count(toCodon.Triplet, "G") + strings.Count(toCodon.Triplet, "C")
					switch {
					case codonBias == toCodonBias:
						db.MustExec(`INSERT INTO codonbias(fromcodon, tocodon, gcbias) VALUES (?, ?, ?)`, codon.Triplet, toCodon.Triplet, "NA")
					case codonBias > toCodonBias:
						db.MustExec(`INSERT INTO codonbias(fromcodon, tocodon, gcbias) VALUES (?, ?, ?)`, codon.Triplet, toCodon.Triplet, "AT")
					case codonBias < toCodonBias:
						db.MustExec(`INSERT INTO codonbias(fromcodon, tocodon, gcbias) VALUES (?, ?, ?)`, codon.Triplet, toCodon.Triplet, "GC")
					}
				}
			}
		}
	}

	// Insert seq and history

	if len(sequence)%3 != 0 {
		return "", []Change{}, errors.New("THE SEQUENCE ISN'T A COMPLETE CDS, PLEASE TRY TO USE A CDS WITH NOT INTERRUPTED CODONS")
	}

	pos := 0
	for i := 0; i < len(sequence); i = i + 3 {
		codon := sequence[i : i+3]
		db.MustExec(`INSERT INTO seq(pos) VALUES (?)`, pos)
		db.MustExec(`INSERT INTO history(pos, codon, step) VALUES (?, ?, 0)`, pos, codon)
		db.MustExec(`INSERT INTO weights(pos, codon, weight) VALUES (?,?,?)`, pos, codon, weightTable[codon])
		pos++
	}

	var err error
	// For a maximum of 100 iterations, see if we can do better. Usually sequences will be solved within 1-3 rounds,
	// so 100 just effectively acts as the max cap for iterations. Once you get to 100, you pretty much know that
	// we cannot fix the sequence.
	for i := 1; i < FixIterations; i++ {
		suggestions := findProblems(sequence, problematicSequenceFuncs)
		// If there are no suggestions, break the iteration!
		if len(suggestions) == 0 {
			// Add a historical log of changes
			var changes []Change
			_ = db.Select(&changes, `SELECT h.pos AS position, h.step AS step, (SELECT codon FROM history WHERE pos = h.pos AND step = h.step-1 LIMIT 1) AS codonfrom, h.codon AS codonto, sf.suggestiontype AS reason FROM history AS h JOIN suggestedfix AS sf ON sf.id = h.suggestedfix WHERE h.suggestedfix IS NOT NULL ORDER BY h.step, h.pos`)
			return sequence, changes, nil
		}
		for _, suggestion := range suggestions { // if you want to add overlaps, add suggestionIndex
			// First, let's insert the suggestions that we found using our problematicSequenceFuncs
			_, err = db.Exec(`INSERT INTO suggestedfix(step, start, end, gcbias, quantityfixes, suggestiontype) VALUES (?, ?, ?, ?, ?, ?)`, i, suggestion.Start, suggestion.End, suggestion.Bias, suggestion.QuantityFixes, suggestion.SuggestionType)
			if err != nil {
				return sequence, []Change{}, err
			}
		}

		// The following statements are the magic sauce that makes this all worthwhile.
		// Parameters: step, gcbias, start, end, quantityfix
		sqlFix1 := `INSERT INTO history
		            (codon,
		             pos,
		             step,
			     suggestedfix)
		SELECT t.codon,
		       t.pos,
		       ? AS step,
		       ? AS suggestedfix
		FROM   (SELECT cb.tocodon AS codon,
		               s.pos      AS pos
		        FROM   seq AS s
		               JOIN history AS h
		                 ON h.pos = s.pos
		               JOIN weights AS w
		                 ON w.pos = s.pos
		               JOIN codon AS c
		                 ON h.codon = c.codon
		               JOIN codonbias AS cb
		                 ON cb.fromcodon = c.codon
		        WHERE `
		sqlFix2 := ` s.pos >= ?
		               AND s.pos <= ?
		               AND h.codon != cb.tocodon
		        ORDER  BY w.weight) AS t
		GROUP  BY t.pos
		LIMIT  ?; `

		independentSuggestions := []dbDnaSuggestion{}
		_ = db.Select(&independentSuggestions, `SELECT * FROM suggestedfix WHERE step = ?`, i)

		for _, independentSuggestion := range independentSuggestions {
			switch independentSuggestion.Bias {
			case "NA":
				db.MustExec(sqlFix1+sqlFix2, i, independentSuggestion.ID, independentSuggestion.Start, independentSuggestion.End, independentSuggestion.QuantityFixes)
			case "GC":
				db.MustExec(sqlFix1+`cb.gcbias = 'GC' AND `+sqlFix2, i, independentSuggestion.ID, independentSuggestion.Start, independentSuggestion.End, independentSuggestion.QuantityFixes)
			case "AT":
				db.MustExec(sqlFix1+`cb.gcbias = 'AT' AND `+sqlFix2, i, independentSuggestion.ID, independentSuggestion.Start, independentSuggestion.End, independentSuggestion.QuantityFixes)
			}
		}
		var codons []string
		_ = db.Select(&codons, `SELECT codon FROM (SELECT codon, pos FROM history ORDER BY step DESC) GROUP BY pos`)
		sequence = strings.Join(codons, "")
	}

	var changes []Change
	_ = db.Select(&changes, `SELECT h.pos AS position, h.step AS step, (SELECT codon FROM history WHERE pos = h.pos AND step = h.step-1 LIMIT 1) AS codonfrom, h.codon AS codonto, sf.suggestiontype AS reason FROM history AS h JOIN suggestedfix AS sf ON sf.id = h.suggestedfix WHERE h.suggestedfix IS NOT NULL ORDER BY h.step, h.pos`)

	if len(changes) > 0 {
		return sequence, changes, nil
	}

	return sequence, []Change{}, errors.New("Could not find a solution to sequence space")
}

// FixCdsSimple is FixCds with some defaults for normal usage, including
// finding homopolymers, finding repeats, and ensuring a normal range of GC
// content. It also allows users to put in sequences that they do not wish to
// occur within their CDS, like restriction enzyme cut sites.
func FixCdsSimple(sequence string, codontable codon.Table, sequencesToRemove []string) (string, []Change, error) {
	var functions []func(string, chan DnaSuggestion, *sync.WaitGroup)
	// Remove homopolymers
	functions = append(functions, RemoveSequence([]string{"AAAAAAAA", "GGGGGGGG"}))

	// Remove user defined sequences
	functions = append(functions, RemoveSequence(sequencesToRemove))

	// Remove repeats
	functions = append(functions, RemoveRepeat(18))

	// Ensure normal GC range

	return FixCds(":memory:", sequence, codontable, functions)
}

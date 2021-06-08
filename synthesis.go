package poly

import (
	"github.com/jmoiron/sqlx"
	//"github.com/juliangruber/go-intersect"
	_ "github.com/mattn/go-sqlite3"
	"regexp"
	"strings"
	"sync"
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
do that for 1000 rounds, at which point we just give up and return the sequence to the user as is.

Hopefully, this will enable more high throughput synthesis of genes!

Keoni

[started Dec 9, 2020]
******************************************************************************/

// DnaSuggestion is a suggestion of a fixer, generated by a problematicSequenceFunc.
type DnaSuggestion struct {
	Start          int    `db:"start"`
	End            int    `db:"end"`
	Bias           string `db:"gcbias"`
	QuantityFixes  int    `db:"quantityfixes"`
	SuggestionType string `db:"suggestiontype"`
	Step           int    `db:"step"`
	Id             int    `db:"id"`
}

// FindBsaI is a simple problematicSequenceFunc, for use in testing
func FindBsaI(sequence string, c chan DnaSuggestion, wg *sync.WaitGroup) {
	re := regexp.MustCompile(`GGTCTC`)
	locs := re.FindAllStringIndex(sequence, -1)
	for _, loc := range locs {
		position := loc[0] / 3
		leftover := loc[0] % 3
		switch {
		case leftover == 0:
			c <- DnaSuggestion{position, (loc[1] / 3), "NA", 1, "BsaI removal", 0, 0}
		case leftover != 0:
			c <- DnaSuggestion{position, (loc[1] / 3) - 1, "NA", 1, "BsaI removal", 0, 0}
		}
	}
	wg.Done()
}

// FindTypeIIS is a problematicSequenceFunc used for finding TypeIIS restriction enzymes. It finds BbsI, BsaI, BtgZI, BsmBI, SapI, and PaqCI(AarI)
func FindTypeIIS(sequence string, c chan DnaSuggestion, wg *sync.WaitGroup) {
	enzymeSites := []string{"GAAGAC", "GGTCTC", "GCGATG", "CGTCTC", "GCTCTTC", "CACCTGC"}
	var enzymes []string
	for _, enzyme := range enzymeSites {
		enzymes = []string{enzyme, ReverseComplement(enzyme)}
		for _, site := range enzymes {
			re := regexp.MustCompile(site)
			locs := re.FindAllStringIndex(sequence, -1)
			for _, loc := range locs {
				position := loc[0] / 3
				leftover := loc[0] % 3
				switch {
				case leftover == 0:
					c <- DnaSuggestion{position, (loc[1] / 3), "NA", 1, "TypeIIS removal", 0, 0}
				case leftover != 0:
					c <- DnaSuggestion{position, (loc[1] / 3) - 1, "NA", 1, "TypeIIS removal", 0, 0}
				}
			}
		}
	}
	wg.Done()
}

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
func FixCds(sqlitePath string, sequence string, codontable CodonTable, problematicSequenceFuncs []func(string, chan DnaSuggestion, *sync.WaitGroup)) (string, error) {
	db := sqlx.MustConnect("sqlite3", sqlitePath)
	createMemoryDbSql := `
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
		step INT
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
	db.MustExec(createMemoryDbSql)
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
	pos := 0
	for i := 0; i < len(sequence)-3; i = i + 3 {
		codon := sequence[i : i+3]
		db.MustExec(`INSERT INTO seq(pos) VALUES (?)`, pos)
		db.MustExec(`INSERT INTO history(pos, codon, step) VALUES (?, ?, 0)`, pos, codon)
		db.MustExec(`INSERT INTO weights(pos, codon, weight) VALUES (?,?,?)`, pos, codon, weightTable[codon])
		pos++
	}

	var err error
	// For a maximum of 100 iterations, see if we can do better
	for i := 1; i < 100; i++ {
		suggestions := findProblems(sequence, problematicSequenceFuncs)
		// If there are no suggestions, break the iteration!
		if len(suggestions) == 0 {
			break
		}
		for _, suggestion := range suggestions { // if you want to add overlaps, add suggestionIndex
			// First, let's insert the suggestions that we found using our problematicSequenceFuncs
			_, err = db.Exec(`INSERT INTO suggestedfix(step, start, end, gcbias, quantityfixes, suggestiontype) VALUES (?, ?, ?, ?, ?, ?)`, i, suggestion.Start, suggestion.End, suggestion.Bias, suggestion.QuantityFixes, suggestion.SuggestionType)
			if err != nil {
				return sequence, err
			}
		}

		// The following statements are the magic sauce that makes this all worthwhile.
		// Parameters: step, gcbias, start, end, quantityfix
		sqlFix1 := `INSERT INTO history
		            (codon,
		             pos,
		             step)
		SELECT t.codon,
		       t.pos,
		       ? AS step
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

		independentSuggestions := []DnaSuggestion{}
		_ = db.Select(&independentSuggestions, `SELECT * FROM suggestedfix WHERE step = ?`, i)

		for _, independentSuggestion := range independentSuggestions {
			switch independentSuggestion.Bias {
			case "NA":
				db.MustExec(sqlFix1+sqlFix2, i, independentSuggestion.Start, independentSuggestion.End, independentSuggestion.QuantityFixes)
			case "GC":
				db.MustExec(sqlFix1+`cb.bias = 'GC' AND `+sqlFix2, i, independentSuggestion.Start, independentSuggestion.End, independentSuggestion.QuantityFixes)
			case "AT":
				db.MustExec(sqlFix1+`cb.bias = 'AT' AND `+sqlFix2, i, independentSuggestion.Start, independentSuggestion.End, independentSuggestion.QuantityFixes)
			}
		}
		var codons []string
		_ = db.Select(&codons, `SELECT codon FROM (SELECT codon, pos FROM history ORDER BY step DESC) GROUP BY pos`)
		sequence = strings.Join(codons, "")
	}
	return sequence, nil
}

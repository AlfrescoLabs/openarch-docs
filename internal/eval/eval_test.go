package eval

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	qs, err := Parse(strings.NewReader("# set\nQ: ¿Quién?\nE: instalador | Rubén; correo\nN: cliente\n\nQ: ¿Catastro?\nS: needs the web\n"))
	if err != nil || len(qs) != 2 {
		t.Fatalf("Parse = %+v, %v", qs, err)
	}
	if len(qs[0].Expect) != 2 || qs[0].Expect[0][1] != "Rubén" || qs[0].Forbid[0] != "cliente" || qs[1].Skip != "needs the web" {
		t.Errorf("questions = %+v", qs)
	}
	for _, bad := range []string{"E: x\n", "Q: a\nX: y\n", "Q: a\nnot a key\n"} {
		if _, err := Parse(strings.NewReader(bad)); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestScore(t *testing.T) {
	q := Question{Expect: [][]string{{"instalador", "ruben"}, {"correo"}, {"12/03"}}, Forbid: []string{"cliente envió"}}
	for answer, want := range map[string]string{
		"Lo envió Rubén por correo el 12/03.":        Pass,
		"Lo envió el INSTALADOR por correo.":           Partial,
		"No consta.":                                   Fail,
		"El cliente envió el correo el 12/03, Rubén": Fail,
	} {
		if got, _, _ := Score(q, answer); got != want {
			t.Errorf("Score(%q) = %s, want %s", answer, got, want)
		}
	}
}

func TestJudgeScore(t *testing.T) {
	for reply, want := range map[string]int{
		`{"facts":[{"fact":"a","stated":true},{"fact":"b","stated":true}]}`:                                     2,
		"Here:\n```json\n{\"facts\":[{\"fact\":\"a\",\"stated\":true},{\"fact\":\"b\",\"stated\":false}]}\n```": 1,
		`{"facts":[{"fact":"a","stated":true},{"fact":"b","stated":true,"contradicted":true}]}`:                 0,
		`{"facts":[{"fact":"a","stated":false},{"fact":"b","stated":false},{"fact":"c","stated":true}]}`:        0,
		"2": -1,
	} {
		if got, _ := judgeScore(reply); got != want {
			t.Errorf("judgeScore(%q) = %d, want %d", reply, got, want)
		}
	}
}

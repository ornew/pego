package grammar

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

// A deterministic cancellation point avoids relying on validation timing.
type validationPollContext struct {
	context.Context
	cancel context.CancelFunc
	left   int
}

func (c *validationPollContext) Err() error {
	c.left--
	if c.left == 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestValidateContextCancellationWithinWideNodes(t *testing.T) {
	statements := make([]Decl, 4096)
	for i := range statements {
		statements[i] = &RuleDef{Name: "item", Expr: &Top{}}
	}
	ranges := make([]CharRange, 4096)
	for i := range ranges {
		ranges[i] = CharRange{Lo: 'a', Hi: 'z'}
	}
	for _, tc := range []struct {
		name string
		g    *Grammar
	}{
		{"wide statement list", &Grammar{File: File{Decls: statements}}},
		{"wide character class", expressionGrammar(&CharClass{Ranges: ranges})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			poll := &validationPollContext{Context: ctx, cancel: cancel, left: 2}
			if err := ValidateContext(poll, tc.g); !errors.Is(err, context.Canceled) {
				t.Fatalf("validation returned %v, want cancellation", err)
			}
			if poll.left > 0 {
				t.Fatal("validation did not poll during the wide node")
			}
			if err := Validate(tc.g); err != nil {
				t.Fatalf("cancellation mutated the grammar: %v", err)
			}
		})
	}
}

func TestValidateContextDiagnosticParity(t *testing.T) {
	for _, g := range []*Grammar{
		expressionGrammar(&Seq{Items: []Expr{&Top{}, &Repeat{Expr: &Top{}, Min: 2, Max: 1}}}),
		expressionGrammar(&Seq{Items: []Expr{&Top{}, nil}}),
		expressionGrammar(&Choice{Alts: []Expr{&Top{}, &Literal{Value: "a"}}}),
	} {
		ctx, cancel := context.WithCancel(context.Background())
		got := ValidateContext(ctx, g)
		cancel()
		if want := Validate(g); !reflect.DeepEqual(got, want) {
			t.Fatalf("validation differs: got %v, want %v", got, want)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ValidateContext(ctx, expressionGrammar(&Top{})); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled validation returned %v", err)
	}
}

func TestValidateContextPollsAttributeIdentifierScan(t *testing.T) {
	kwargs := make(map[string]any, 4098)
	for i := range 4096 {
		kwargs[fmt.Sprintf("key%d", i)] = i
	}
	kwargs["z-bad"] = 1
	kwargs["a-bad"] = 1
	attribute := &Attribute{Name: "error", KwArgs: kwargs}
	if field, _ := invalidIdentifier(attribute, nil); field != `kwArgs["a-bad"]` {
		t.Fatalf("identifier diagnostic order changed: %s", field)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	poll := &validationPollContext{Context: ctx, cancel: cancel, left: 1}
	var failure any
	func() {
		defer func() { failure = recover() }()
		invalidIdentifier(attribute, &validationControl{ctx: poll})
	}()
	cancelled, ok := failure.(validationCancellation)
	if !ok || !errors.Is(cancelled.err, context.Canceled) {
		t.Fatalf("attribute key scan was not interrupted: %v", failure)
	}
}

func TestValidateContextCharacterRangeDiagnostics(t *testing.T) {
	for _, index := range []int{255, 256, 512} {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			ranges := make([]CharRange, 513)
			for i := range ranges {
				ranges[i] = CharRange{Lo: 'a', Hi: 'z'}
			}
			ranges[index] = CharRange{Lo: 'z', Hi: 'a'}
			g := expressionGrammar(&CharClass{Ranges: ranges})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if got, want := ValidateContext(ctx, g), Validate(g); !reflect.DeepEqual(got, want) {
				t.Fatalf("range diagnostic differs: got %v, want %v", got, want)
			}
		})
	}
}

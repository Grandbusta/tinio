package mind

import (
	"cmp"
	"context"
	"slices"

	"github.com/Grandbusta/tinio/internal/providers/embed"
)

type alike[T any] struct {
	item T
	sim  float64 // cosine
}

// rankAlike returns up to limit of items at least minSim alike to about, most
// alike first. vector gives an item's embedding and the model that made it.
// Items with none yet, or one from another model (the embedder was changed),
// are embedded in the same call and saved, so each is embedded once per model.
func rankAlike[T any](ctx context.Context, m *Mind, about string, items []T, minSim float64, limit int,
	vector func(*T) (*[]float32, string), text func(T) string, save func(T, string) error) ([]alike[T], embed.Embedded, error) {
	texts, model := []string{about}, m.Embedder.Model()
	var missing []int
	for i := range items {
		if v, by := vector(&items[i]); len(*v) == 0 || by != model {
			texts, missing = append(texts, text(items[i])), append(missing, i)
		}
	}
	out, err := m.Embedder.Embed(ctx, texts)
	if err != nil {
		return nil, out, err
	}
	for n, i := range missing {
		v, _ := vector(&items[i])
		*v = out.Vectors[n+1]
		if err := save(items[i], out.Model); err != nil {
			m.Log.Warn("saving an embedding failed", "err", err)
		}
	}
	var ranked []alike[T]
	for i := range items {
		v, _ := vector(&items[i])
		if sim := embed.Similarity(out.Vectors[0], *v); sim >= minSim {
			ranked = append(ranked, alike[T]{items[i], sim})
		}
	}
	slices.SortStableFunc(ranked, func(a, b alike[T]) int { return cmp.Compare(b.sim, a.sim) })
	return ranked[:min(len(ranked), limit)], out, nil
}

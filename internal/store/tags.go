package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"cloud.google.com/go/firestore"
	"my-bookmark/internal/summary"
)

// maxTagScan bounds how many of a user's bookmarks are read to build the tag list.
const maxTagScan = 2000

// TagCount is a tag used by a user and how many of their bookmarks carry it.
type TagCount struct {
	Name  string
	Count int
}

// Tags returns the user's tags, most used first, at most limit of them.
func (s *Store) Tags(ctx context.Context, uid string, limit int) ([]TagCount, error) {
	docs, err := s.client.Collection("bookmarks").Where("user_id", "==", uid).Select("tags").Limit(maxTagScan).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	return countTags(docs, limit)
}

func countTags(docs []*firestore.DocumentSnapshot, limit int) ([]TagCount, error) {
	counts := map[string]int{}
	for _, d := range docs {
		var b Bookmark
		if err := d.DataTo(&b); err != nil {
			return nil, err
		}
		for _, tag := range b.Tags {
			counts[tag]++
		}
	}
	tags := make([]TagCount, 0, len(counts))
	for name, n := range counts {
		tags = append(tags, TagCount{Name: name, Count: n})
	}
	sort.Slice(tags, func(i, j int) bool {
		if tags[i].Count != tags[j].Count {
			return tags[i].Count > tags[j].Count
		}
		return tags[i].Name < tags[j].Name
	})
	if limit > 0 && len(tags) > limit {
		tags = tags[:limit]
	}
	return tags, nil
}

// maxLearnedTags bounds how many tags people added by hand are offered to
// automatic tagging, so the prompt and response schema stay small.
const maxLearnedTags = 50

// LearnedTag is a tag someone added by hand that automatic tagging may reuse.
type LearnedTag struct {
	Name       string    `firestore:"name"`
	LastUsedAt time.Time `firestore:"last_used_at"`
}

func (s *Store) learnedTag(name string) *firestore.DocumentRef {
	return s.client.Collection("tag_vocabulary").Doc(Hash(name))
}

func (s *Store) configuredTag(name string) bool {
	for _, candidate := range s.tagCandidates {
		if name == candidate {
			return true
		}
	}
	return false
}

// TagVocabulary returns the configured candidates followed by the most
// recently used hand-added tags. Automatic tagging stays off without at least
// two configured candidates.
func (s *Store) TagVocabulary(ctx context.Context) ([]string, error) {
	if len(s.tagCandidates) < 2 {
		return nil, nil
	}
	docs, err := s.client.Collection("tag_vocabulary").OrderBy("last_used_at", firestore.Desc).Limit(maxLearnedTags).Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("list learned tags: %w", err)
	}
	vocabulary := append([]string(nil), s.tagCandidates...)
	for _, d := range docs {
		var t LearnedTag
		if err := d.DataTo(&t); err != nil {
			return nil, err
		}
		if t.Name != "" && !s.configuredTag(t.Name) {
			vocabulary = append(vocabulary, t.Name)
		}
	}
	return vocabulary, nil
}

// knownAutoTags accepts generated tags drawn from the configured candidates or
// from any tag someone has added by hand, even one that has since fallen out
// of the offered window, so older suggestions are not regenerated needlessly.
func (s *Store) knownAutoTags(tx *firestore.Transaction, tags []string) (bool, error) {
	if len(s.tagCandidates) < 2 || !summary.ValidAutoTags(tags, tags) {
		return false, nil
	}
	for _, tag := range tags {
		if s.configuredTag(tag) {
			continue
		}
		if _, err := tx.Get(s.learnedTag(tag)); err != nil {
			if missing(err) {
				return false, nil
			}
			return false, err
		}
	}
	return true, nil
}

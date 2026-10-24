package store

import (
	"context"
	"sort"

	"cloud.google.com/go/firestore"
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

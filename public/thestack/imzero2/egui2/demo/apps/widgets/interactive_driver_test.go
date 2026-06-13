package widgets

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

func mkDemo(name, category string) (d registry.Demo) {
	d = registry.Demo{Name: name, Category: category, Title: name}
	return
}

func TestGalleryGroupByCategory_EmptyCategoryBucket(t *testing.T) {
	demos := []registry.Demo{mkDemo("solo", "")}
	groups := galleryGroupByCategory(demos, "")
	require.Len(t, groups, 1)
	assert.Equal(t, "(other)", groups[0].category)
}

func TestGalleryGroupByCategory_SortedCategoriesAndDemos(t *testing.T) {
	demos := []registry.Demo{
		mkDemo("charlie", "tools"),
		mkDemo("alpha", "tools"),
		mkDemo("bravo", "system"),
	}
	groups := galleryGroupByCategory(demos, "")
	require.Len(t, groups, 2)
	assert.Equal(t, "system", groups[0].category)
	assert.Equal(t, "tools", groups[1].category)
	require.Len(t, groups[1].demos, 2)
	assert.Equal(t, "alpha", groups[1].demos[0].Name)
	assert.Equal(t, "charlie", groups[1].demos[1].Name)
}

func TestGalleryGroupByCategory_FilterByName(t *testing.T) {
	demos := []registry.Demo{
		mkDemo("regex", "tools"),
		mkDemo("hn", "tools"),
	}
	groups := galleryGroupByCategory(demos, "regex")
	require.Len(t, groups, 1)
	require.Len(t, groups[0].demos, 1)
	assert.Equal(t, "regex", groups[0].demos[0].Name)
}

func TestGalleryGroupByCategory_FilterByCategory(t *testing.T) {
	demos := []registry.Demo{
		mkDemo("regex", "tools"),
		mkDemo("topbar", "system"),
	}
	groups := galleryGroupByCategory(demos, "syst")
	require.Len(t, groups, 1)
	assert.Equal(t, "system", groups[0].category)
}

func TestGalleryGroupByCategory_FilterNoMatch(t *testing.T) {
	demos := []registry.Demo{mkDemo("foo", "tools")}
	groups := galleryGroupByCategory(demos, "nomatch")
	assert.Empty(t, groups)
}

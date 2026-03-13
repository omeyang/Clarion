package schema

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/omeyang/clarion/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestContactFromModel(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)

	m := &model.Contact{
		ID:            1,
		PhoneMasked:   "138****1234",
		PhoneHash:     "sha256:abc123",
		Source:        "crm_import",
		ProfileJSON:   json.RawMessage(`{"age":30,"city":"北京"}`),
		CurrentStatus: "active",
		DoNotCall:     false,
		CreatedAt:     now,
		UpdatedAt:     now.Add(time.Hour),
	}

	got := ContactFromModel(m)

	assert.Equal(t, m.ID, got.ID)
	assert.Equal(t, m.PhoneMasked, got.PhoneMasked)
	assert.Equal(t, m.PhoneHash, got.PhoneHash)
	assert.Equal(t, m.Source, got.Source)
	assert.JSONEq(t, string(m.ProfileJSON), string(got.ProfileJSON))
	assert.Equal(t, m.CurrentStatus, got.CurrentStatus)
	assert.Equal(t, m.DoNotCall, got.DoNotCall)
	assert.Equal(t, m.CreatedAt, got.CreatedAt)
	assert.Equal(t, m.UpdatedAt, got.UpdatedAt)
}

func TestContactFromModel_DoNotCall(t *testing.T) {
	t.Parallel()

	m := &model.Contact{
		ID:        2,
		DoNotCall: true,
	}

	got := ContactFromModel(m)
	assert.True(t, got.DoNotCall)
}

func TestContactFromModel_NilProfileJSON(t *testing.T) {
	t.Parallel()

	m := &model.Contact{
		ID:          3,
		ProfileJSON: nil,
	}

	got := ContactFromModel(m)
	assert.Nil(t, got.ProfileJSON)
}

func TestContactsFromModels(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)

	contacts := []model.Contact{
		{ID: 1, PhoneMasked: "138****1111", Source: "api", CreatedAt: now},
		{ID: 2, PhoneMasked: "139****2222", Source: "import", CreatedAt: now},
		{ID: 3, PhoneMasked: "137****3333", Source: "crm", CreatedAt: now},
	}

	got := ContactsFromModels(contacts)

	assert.Len(t, got, 3)
	for i, c := range contacts {
		assert.Equal(t, c.ID, got[i].ID)
		assert.Equal(t, c.PhoneMasked, got[i].PhoneMasked)
		assert.Equal(t, c.Source, got[i].Source)
	}
}

func TestContactsFromModels_Empty(t *testing.T) {
	t.Parallel()

	got := ContactsFromModels([]model.Contact{})
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

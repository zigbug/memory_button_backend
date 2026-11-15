package handlers

import "memorybutton/backend/internal/domain"

// ---------- Auth ----------

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token     string     `json:"token"`
	TokenType string     `json:"token_type"`
	ExpiresIn int64      `json:"expires_in"`
	User      domain.User `json:"user"`
}

// ---------- Persons ----------

type PersonRequest struct {
	DisplayName  string   `json:"display_name"`
	NameVariants []string `json:"name_variants"`
	BirthDate    string   `json:"birth_date"`
	BirthApprox  bool     `json:"birth_approx"`
	DeathDate    string   `json:"death_date"`
	DeathApprox  bool     `json:"death_approx"`
	Gender       string   `json:"gender"`
	Places       []string `json:"places"`
	Notes        string   `json:"notes"`
	PhotoMediaID *string  `json:"photo_media_id"`
}

func (p PersonRequest) toDomain() domain.Person {
	return domain.Person{
		DisplayName:  p.DisplayName,
		NameVariants: p.NameVariants,
		BirthDate:    p.BirthDate,
		BirthApprox:  p.BirthApprox,
		DeathDate:    p.DeathDate,
		DeathApprox:  p.DeathApprox,
		Gender:       p.Gender,
		Places:       p.Places,
		Notes:        p.Notes,
		PhotoMediaID: p.PhotoMediaID,
	}
}

// ---------- Relationships ----------

type RelationshipRequest struct {
	FromPersonID string `json:"from_person_id"`
	ToPersonID   string `json:"to_person_id"`
	Type         string `json:"type"`
	Confidence   string `json:"confidence"`
	Note         string `json:"note"`
}

func (r RelationshipRequest) toDomain(source string) domain.Relationship {
	conf := domain.Confidence(r.Confidence)
	if conf == "" {
		conf = domain.ConfidenceMedium
	}
	return domain.Relationship{
		FromPersonID: r.FromPersonID,
		ToPersonID:   r.ToPersonID,
		Type:         domain.RelationshipType(r.Type),
		Confidence:   conf,
		Source:       source,
		Note:         r.Note,
	}
}

// ---------- Sessions ----------

type CreateSessionRequest struct {
	Mode       string   `json:"mode"`
	PersonID   *string  `json:"person_id"`
	QuestionIDs []string `json:"question_ids"`
}

// ---------- Drafts / stories ----------

type ConfirmDraftRequest struct {
	Items          []domain.DraftDecision `json:"items"`
	StoryText      string                 `json:"story_text"`
	StoryPersonIDs []string               `json:"story_person_ids"`
}

type ConfirmStoryRequest struct {
	Text      string   `json:"text"`
	PersonIDs []string `json:"person_ids"`
}

// ---------- Questions ----------

type CreateQuestionRequest struct {
	PersonID         *string `json:"person_id"`
	Text             string  `json:"text"`
	Reason           string  `json:"reason"`
	Priority         int     `json:"priority"`
	RelatedFactID    *string `json:"related_fact_id"`
	RelatedSessionID *string `json:"related_session_id"`
}

type UpdateQuestionRequest struct {
	Text     *string `json:"text"`
	Status   *string `json:"status"`
	Priority *int    `json:"priority"`
	Reason   *string `json:"reason"`
}

// ---------- Q&A ----------

type QARequest struct {
	Question      string  `json:"question"`
	ScopePersonID *string `json:"scope_person_id"`
}

// ---------- Media ----------

type PresignRequest struct {
	Kind      string `json:"kind"`
	OwnerKind string `json:"owner_kind"`
	OwnerID   string `json:"owner_id"`
	Filename  string `json:"filename"`
	MIME      string `json:"mime"`
}

type PresignResponse struct {
	StorageKey string `json:"storage_key"`
	UploadURL  string `json:"upload_url"`
	Method     string `json:"method"`
	Headers    map[string]string `json:"headers"`
}

type RegisterMediaRequest struct {
	Kind       string `json:"kind"`
	OwnerKind  string `json:"owner_kind"`
	OwnerID    string `json:"owner_id"`
	StorageKey string `json:"storage_key"`
	MIME       string `json:"mime"`
	SizeBytes  int64  `json:"size_bytes"`
	Checksum   string `json:"checksum"`
}

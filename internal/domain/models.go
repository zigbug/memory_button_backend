// Package domain содержит основные сущности предметной области Memory Button.
package domain

import "time"

// ---------- Режимы и статусы ----------

type SessionMode string

const (
	ModeNewStory       SessionMode = "new_story"
	ModeContinuePerson SessionMode = "continue_person"
	ModeClarifyPerson  SessionMode = "clarify_person"
	ModeQA             SessionMode = "qa"
)

type SessionStatus string

const (
	SessionActive    SessionStatus = "active"
	SessionCompleted SessionStatus = "completed"
	SessionArchived  SessionStatus = "archived"
)

type TurnRole string

const (
	RoleUser      TurnRole = "user"
	RoleAssistant TurnRole = "assistant"
	RoleSystem    TurnRole = "system"
)

type RelationshipType string

const (
	RelParentChild RelationshipType = "parent_child"
	RelSpouse      RelationshipType = "spouse"
	RelSibling     RelationshipType = "sibling"
	RelOther       RelationshipType = "other"
)

type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

type MediaKind string

const (
	MediaAudio MediaKind = "audio"
	MediaPhoto MediaKind = "photo"
	MediaOther MediaKind = "other"
)

type StoryStatus string

const (
	StoryDraft    StoryStatus = "draft"
	StoryConfirmed StoryStatus = "confirmed"
)

type QuestionStatus string

const (
	QuestionOpen     QuestionStatus = "open"
	QuestionResolved QuestionStatus = "resolved"
	QuestionDismissed QuestionStatus = "dismissed"
)

type QuestionSource string

const (
	SourceDialogue   QuestionSource = "dialogue"
	SourceQA         QuestionSource = "qa"
	SourceExtraction QuestionSource = "extraction"
	SourceManual     QuestionSource = "manual"
)

type DraftStatus string

const (
	DraftPending   DraftStatus = "pending"
	DraftConfirmed DraftStatus = "confirmed"
	DraftDismissed DraftStatus = "dismissed"
)

type DraftItemKind string

const (
	ItemPerson       DraftItemKind = "person"
	ItemRelationship DraftItemKind = "relationship"
	ItemFact         DraftItemKind = "fact"
	ItemQuestion     DraftItemKind = "question"
	ItemMediaLink    DraftItemKind = "media_link"
	ItemStory        DraftItemKind = "story"
)

type DraftItemAction string

const (
	ActionCreate DraftItemAction = "create"
	ActionUpdate DraftItemAction = "update"
	ActionLink   DraftItemAction = "link"
)

type JobStatus string

const (
	JobPending    JobStatus = "pending"
	JobProcessing JobStatus = "processing"
	JobDone       JobStatus = "done"
	JobFailed     JobStatus = "failed"
)

// ---------- Сущности ----------

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	CreatedAt    time.Time `json:"created_at"`
}

type Person struct {
	ID           string    `json:"id"`
	DisplayName  string    `json:"display_name"`
	NameVariants []string  `json:"name_variants"`
	BirthDate    string    `json:"birth_date,omitempty"`
	BirthApprox  bool      `json:"birth_approx"`
	DeathDate    string    `json:"death_date,omitempty"`
	DeathApprox  bool      `json:"death_approx"`
	Gender       string    `json:"gender,omitempty"`
	Places       []string  `json:"places"`
	Notes        string    `json:"notes"`
	PhotoMediaID *string   `json:"photo_media_id,omitempty"`
	PhotoURL     string    `json:"photo_url,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Relationship struct {
	ID            string           `json:"id"`
	FromPersonID  string           `json:"from_person_id"`
	ToPersonID    string           `json:"to_person_id"`
	FromPersonName string          `json:"from_person_name,omitempty"`
	ToPersonName  string           `json:"to_person_name,omitempty"`
	Type          RelationshipType `json:"type"`
	Confidence    Confidence       `json:"confidence"`
	Source        string           `json:"source"`
	Note          string           `json:"note"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

type Session struct {
	ID             string        `json:"id"`
	Mode           SessionMode   `json:"mode"`
	PersonID       *string       `json:"person_id,omitempty"`
	PersonName     string        `json:"person_name,omitempty"`
	Status         SessionStatus `json:"status"`
	ContextSummary string        `json:"context_summary,omitempty"`
	QuestionIDs    []string      `json:"question_ids,omitempty"`
	StartedAt      time.Time     `json:"started_at"`
	EndedAt        *time.Time    `json:"ended_at,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

type Turn struct {
	ID            string    `json:"id"`
	SessionID     string    `json:"session_id"`
	Seq           int       `json:"seq"`
	Role          TurnRole  `json:"role"`
	Text          string    `json:"text"`
	AudioMediaID  *string   `json:"audio_media_id,omitempty"`
	AudioURL      string    `json:"audio_url,omitempty"`
	Engine        string    `json:"engine,omitempty"` // asr/llm/tts provider+model
	Fallback      bool      `json:"fallback,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type TranscriptSegment struct {
	Start   float64 `json:"start,omitempty"`
	End     float64 `json:"end,omitempty"`
	Text    string  `json:"text"`
	Speaker string  `json:"speaker,omitempty"`
}

type Transcript struct {
	ID        string              `json:"id"`
	SessionID string              `json:"session_id"`
	Segments  []TranscriptSegment `json:"segments"`
	Engine    string              `json:"engine"`
	CreatedAt time.Time           `json:"created_at"`
}

type Story struct {
	ID        string       `json:"id"`
	SessionID *string      `json:"session_id,omitempty"`
	PersonIDs []string     `json:"person_ids"`
	Text      string       `json:"text"`
	Status    StoryStatus  `json:"status"`
	Version   int          `json:"version"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type Fact struct {
	ID            string         `json:"id"`
	PersonID      *string        `json:"person_id,omitempty"`
	StoryID       *string        `json:"story_id,omitempty"`
	SessionID     *string        `json:"session_id,omitempty"`
	Type          string         `json:"type"`
	Value         string         `json:"value"`
	Normalized    map[string]any `json:"normalized,omitempty"`
	Confidence    float64        `json:"confidence"`
	SourceRef     string         `json:"source_ref,omitempty"`
	SourceExcerpt string         `json:"source_excerpt,omitempty"`
	Confirmed     bool           `json:"confirmed"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type Question struct {
	ID              string          `json:"id"`
	PersonID        *string         `json:"person_id,omitempty"`
	Text            string          `json:"text"`
	Source          QuestionSource  `json:"source"`
	Reason          string          `json:"reason,omitempty"`
	Status          QuestionStatus  `json:"status"`
	Priority        int             `json:"priority"`
	RelatedFactID   *string         `json:"related_fact_id,omitempty"`
	RelatedSessionID *string        `json:"related_session_id,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	ResolvedAt      *time.Time      `json:"resolved_at,omitempty"`
}

type Media struct {
	ID         string    `json:"id"`
	Kind       MediaKind `json:"kind"`
	StorageKey string    `json:"-"`
	URL        string    `json:"url"`
	MIME       string    `json:"mime"`
	SizeBytes  int64     `json:"size_bytes"`
	Checksum   string    `json:"checksum,omitempty"`
	OwnerKind  string    `json:"owner_kind,omitempty"`
	OwnerID    *string   `json:"owner_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type Draft struct {
	ID          string     `json:"id"`
	SessionID   string     `json:"session_id"`
	Status      DraftStatus `json:"status"`
	StoryText   string     `json:"story_text"`
	Items       []DraftItem `json:"items,omitempty"`
	Meta        map[string]any `json:"meta,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ConfirmedAt *time.Time `json:"confirmed_at,omitempty"`
}

type DraftItem struct {
	ID            string           `json:"id"`
	DraftID       string           `json:"draft_id"`
	Kind          DraftItemKind    `json:"kind"`
	Action        DraftItemAction  `json:"action"`
	Payload       map[string]any   `json:"payload"`
	Status        DraftStatus      `json:"status"`
	ResolvedRefID *string          `json:"resolved_ref_id,omitempty"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

type Job struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Status     JobStatus      `json:"status"`
	Attempts   int            `json:"attempts"`
	LastError  string         `json:"last_error,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// DraftDecision — решение рассказчика по одному пункту черновика.
// Action: "confirm" | "dismiss". Payload может содержать правки перед применением.
type DraftDecision struct {
	ItemID  string         `json:"item_id"`
	Action  string         `json:"action"`
	Payload map[string]any `json:"payload,omitempty"`
}

// ---------- Фильтры ----------

type FactFilter struct {
	PersonID      string
	StoryID       string
	Type          string
	ConfirmedOnly bool
	Limit         int
	Offset        int
}

type QuestionFilter struct {
	PersonID string
	Status   string
	Source   string
	Limit    int
	Offset   int
}

// ---------- Агрегаты и ответы ----------

type PersonCard struct {
	Person        Person        `json:"person"`
	Relationships []Relationship `json:"relationships"`
	Stories       []Story       `json:"stories"`
	Facts         []Fact        `json:"facts"`
	Questions     []Question    `json:"questions"`
}

type QASource struct {
	PersonID  string `json:"person_id,omitempty"`
	PersonName string `json:"person_name,omitempty"`
	StoryID   string `json:"story_id,omitempty"`
	FactID    string `json:"fact_id,omitempty"`
	Excerpt   string `json:"excerpt"`
}

type QAAnswer struct {
	Answer            string     `json:"answer"`
	Sources           []QASource `json:"sources"`
	InsufficientData  bool       `json:"insufficient_data"`
	SuggestedQuestions []string  `json:"suggested_questions,omitempty"`
}

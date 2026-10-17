package domain

// Language is the language a role description is written in (FR-1.1). Empty means unstated: the
// AI Service works it out.
type Language string

const (
	LanguageThai    Language = "th"
	LanguageEnglish Language = "en"
)

// JobOpeningProposal is what proposing a job opening returns for the Recruiter to review (UC-1
// steps 2–3, FR-1.2, FR-1.3): a title and weighted criteria derived from the free-text
// description, and the parts of it that could not be interpreted (FR-1.5). It is never stored —
// the Recruiter corrects it in the browser and confirms it by creating the opening.
type JobOpeningProposal struct {
	Title         string
	Criteria      []Criterion
	Uninterpreted []string
	Attribution   ModelAttribution
}

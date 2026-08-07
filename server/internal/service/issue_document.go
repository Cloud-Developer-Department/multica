package service

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ErrIssueDocumentNotFound is returned when a document row is missing or does
// not belong to the requesting workspace.
var ErrIssueDocumentNotFound = errors.New("issue document not found")

// ErrIssueDocumentVersionConflict is returned when the (issue_id, type,
// version) unique index rejects the insert because a concurrent submit took
// the same computed version first (CLO-283 R6). Callers should surface it as a
// 409 "please retry" instead of a generic 500.
var ErrIssueDocumentVersionConflict = errors.New("issue document version conflict; retry the submission")

// IssueDocumentService owns the write side of the Issue Documents domain
// (CLO-278): registering a new version of an issue-flow document. Read paths
// are served directly by handlers against the generated queries — this service
// exists to make the "insert new version + supersede previous versions" step
// atomic.
type IssueDocumentService struct {
	Queries   *db.Queries
	TxStarter TxStarter
}

func NewIssueDocumentService(q *db.Queries, tx TxStarter) *IssueDocumentService {
	return &IssueDocumentService{Queries: q, TxStarter: tx}
}

// SubmitIssueDocumentParams carries the validated inputs for registering a new
// issue document version. The handler resolves transport inputs into this
// struct (issue must belong to the workspace, actor identity resolved, enum
// values validated).
type SubmitIssueDocumentParams struct {
	WorkspaceID      pgtype.UUID
	IssueID          pgtype.UUID
	Type             string
	Title            string
	Content          string
	ContentType      string
	FileAttachmentID pgtype.UUID
	Status           string
	AuthorType       string
	AuthorID         pgtype.UUID
}

// Submit atomically registers a new version of an issue document:
//  1. computes the next version for (workspace_id, issue_id, type)
//  2. marks every existing version of that (issue_id, type) as `superseded`
//     (history is preserved for the version list)
//  3. inserts the new version
//
// All three steps share one transaction so a failure leaves no partial state.
func (s *IssueDocumentService) Submit(ctx context.Context, p SubmitIssueDocumentParams) (db.IssueDocument, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.IssueDocument{}, err
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	nextVersion, err := qtx.GetIssueDocumentNextVersion(ctx, db.GetIssueDocumentNextVersionParams{
		WorkspaceID: p.WorkspaceID,
		IssueID:     p.IssueID,
		Type:        p.Type,
	})
	if err != nil {
		return db.IssueDocument{}, err
	}

	if err := qtx.SupersedeIssueDocumentByIssueType(ctx, db.SupersedeIssueDocumentByIssueTypeParams{
		WorkspaceID: p.WorkspaceID,
		IssueID:     p.IssueID,
		Type:        p.Type,
	}); err != nil {
		return db.IssueDocument{}, err
	}

	doc, err := qtx.CreateIssueDocument(ctx, db.CreateIssueDocumentParams{
		WorkspaceID:      p.WorkspaceID,
		IssueID:          p.IssueID,
		Type:             p.Type,
		Title:            p.Title,
		Content:          pgtype.Text{String: p.Content, Valid: p.Content != ""},
		ContentType:      p.ContentType,
		FileAttachmentID: p.FileAttachmentID,
		Version:          nextVersion,
		Status:           p.Status,
		AuthorType:       p.AuthorType,
		AuthorID:         p.AuthorID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.IssueDocument{}, ErrIssueDocumentNotFound
		}
		// Concurrent submits of the same (issue, type) both read MAX(version)+1
		// and collide on the idx_issue_document_issue_type_version unique
		// index; the loser surfaces 23505. Translate that into a typed conflict
		// so the handler can answer 409 instead of a misleading 500.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return db.IssueDocument{}, ErrIssueDocumentVersionConflict
		}
		return db.IssueDocument{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return db.IssueDocument{}, err
	}
	return doc, nil
}

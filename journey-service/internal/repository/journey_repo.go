package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/model"
	apperrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/errors"
)

// JourneyRepository handles all database operations for journeys
type JourneyRepository struct {
	db *sql.DB
}

// NewJourneyRepository creates a new repository
func NewJourneyRepository(db *sql.DB) *JourneyRepository {
	return &JourneyRepository{db: db}
}

// RunMigrations executes the SQL migration
func (r *JourneyRepository) RunMigrations(ctx context.Context, sql string) error {
	_, err := r.db.ExecContext(ctx, sql)
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}
	return nil
}

// Create inserts a new journey and its segments in a single transaction
func (r *JourneyRepository) Create(ctx context.Context, j *model.Journey, segments []model.JourneySegment) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return apperrors.DatabaseError("failed to begin transaction", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO journey.journeys (
			journey_id, driver_id, idempotency_key,
			origin_lat, origin_lng, dest_lat, dest_lng,
			departure_time, estimated_arrival, vehicle_type,
			status, rejection_reason, reservation_id,
			version, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		j.JourneyID, j.DriverID, j.IdempotencyKey,
		j.Origin.Lat, j.Origin.Lng, j.Destination.Lat, j.Destination.Lng,
		j.DepartureTime, j.EstimatedArrival, j.VehicleType,
		string(j.Status), j.RejectionReason, j.ReservationID,
		j.Version, j.CreatedAt, j.UpdatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			return apperrors.Conflict("journey with this idempotency key already exists")
		}
		return apperrors.DatabaseError("failed to insert journey", err)
	}

	for _, seg := range segments {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO journey.journey_segments (
				journey_id, segment_id, segment_name, sequence_order,
				time_window_start, time_window_end, traversal_minutes, region
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			j.JourneyID, seg.SegmentID, seg.SegmentName, seg.SequenceOrder,
			seg.TimeWindowStart, seg.TimeWindowEnd, seg.TraversalMinutes, seg.Region,
		)
		if err != nil {
			return apperrors.DatabaseError("failed to insert segment", err)
		}
	}

	return tx.Commit()
}

// GetByID returns a journey and its segments by journey ID
func (r *JourneyRepository) GetByID(ctx context.Context, journeyID string) (*model.Journey, error) {
	j := &model.Journey{}
	var rejReason, reservationID sql.NullString

	err := r.db.QueryRowContext(ctx, `
		SELECT journey_id, driver_id, idempotency_key,
		       origin_lat, origin_lng, dest_lat, dest_lng,
		       departure_time, estimated_arrival, vehicle_type,
		       status, rejection_reason, reservation_id,
		       version, created_at, updated_at,
		       cancelled_at, activated_at, completed_at, expired_at
		FROM journey.journeys
		WHERE journey_id = $1`, journeyID).Scan(
		&j.JourneyID, &j.DriverID, &j.IdempotencyKey,
		&j.Origin.Lat, &j.Origin.Lng, &j.Destination.Lat, &j.Destination.Lng,
		&j.DepartureTime, &j.EstimatedArrival, &j.VehicleType,
		&j.Status, &rejReason, &reservationID,
		&j.Version, &j.CreatedAt, &j.UpdatedAt,
		&j.CancelledAt, &j.ActivatedAt, &j.CompletedAt, &j.ExpiredAt,
	)
	if err == sql.ErrNoRows {
		return nil, apperrors.NotFound("journey not found")
	}
	if err != nil {
		return nil, apperrors.DatabaseError("failed to get journey", err)
	}
	if rejReason.Valid {
		j.RejectionReason = rejReason.String
	}
	if reservationID.Valid {
		j.ReservationID = reservationID.String
	}

	segments, err := r.getSegments(ctx, journeyID)
	if err != nil {
		return nil, err
	}
	j.Segments = segments

	return j, nil
}

func (r *JourneyRepository) getSegments(ctx context.Context, journeyID string) ([]model.JourneySegment, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT segment_id, segment_name, sequence_order,
		       time_window_start, time_window_end, traversal_minutes, region
		FROM journey.journey_segments
		WHERE journey_id = $1
		ORDER BY sequence_order`, journeyID)
	if err != nil {
		return nil, apperrors.DatabaseError("failed to get segments", err)
	}
	defer rows.Close()

	var segments []model.JourneySegment
	for rows.Next() {
		var s model.JourneySegment
		if err := rows.Scan(&s.SegmentID, &s.SegmentName, &s.SequenceOrder,
			&s.TimeWindowStart, &s.TimeWindowEnd, &s.TraversalMinutes, &s.Region); err != nil {
			return nil, apperrors.DatabaseError("failed to scan segment", err)
		}
		segments = append(segments, s)
	}
	return segments, rows.Err()
}

// ListByDriverID returns paginated journeys for a driver
func (r *JourneyRepository) ListByDriverID(ctx context.Context, driverID, statusFilter string, page, limit int) ([]model.Journey, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	where := "WHERE driver_id = $1"
	args := []interface{}{driverID}

	if statusFilter != "" {
		args = append(args, strings.ToUpper(statusFilter))
		where += fmt.Sprintf(" AND status = $%d", len(args))
	}

	var total int64
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM journey.journeys "+where, args...).Scan(&total)
	if err != nil {
		return nil, 0, apperrors.DatabaseError("failed to count journeys", err)
	}

	args = append(args, limit, offset)
	rows, err := r.db.QueryContext(ctx, `
		SELECT journey_id, driver_id, idempotency_key,
		       origin_lat, origin_lng, dest_lat, dest_lng,
		       departure_time, estimated_arrival, vehicle_type,
		       status, rejection_reason, reservation_id,
		       version, created_at, updated_at,
		       cancelled_at, activated_at, completed_at, expired_at
		FROM journey.journeys `+where+`
		ORDER BY created_at DESC
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)),
		args...)
	if err != nil {
		return nil, 0, apperrors.DatabaseError("failed to list journeys", err)
	}
	defer rows.Close()

	return r.scanJourneys(ctx, rows)
}

// AdminList returns all journeys with optional filters
func (r *JourneyRepository) AdminList(ctx context.Context, statusFilter, driverIDFilter string, page, limit int) ([]model.Journey, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	var conditions []string
	var args []interface{}

	if statusFilter != "" {
		args = append(args, strings.ToUpper(statusFilter))
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}
	if driverIDFilter != "" {
		args = append(args, driverIDFilter)
		conditions = append(conditions, fmt.Sprintf("driver_id = $%d", len(args)))
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int64
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM journey.journeys "+where, args...).Scan(&total)
	if err != nil {
		return nil, 0, apperrors.DatabaseError("failed to count journeys", err)
	}

	args = append(args, limit, offset)
	rows, err := r.db.QueryContext(ctx, `
		SELECT journey_id, driver_id, idempotency_key,
		       origin_lat, origin_lng, dest_lat, dest_lng,
		       departure_time, estimated_arrival, vehicle_type,
		       status, rejection_reason, reservation_id,
		       version, created_at, updated_at,
		       cancelled_at, activated_at, completed_at, expired_at
		FROM journey.journeys `+where+`
		ORDER BY created_at DESC
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)),
		args...)
	if err != nil {
		return nil, 0, apperrors.DatabaseError("failed to admin list journeys", err)
	}
	defer rows.Close()

	return r.scanJourneys(ctx, rows)
}

func (r *JourneyRepository) scanJourneys(ctx context.Context, rows *sql.Rows) ([]model.Journey, int64, error) {
	var journeys []model.Journey
	for rows.Next() {
		var j model.Journey
		var rejReason, reservationID sql.NullString
		if err := rows.Scan(
			&j.JourneyID, &j.DriverID, &j.IdempotencyKey,
			&j.Origin.Lat, &j.Origin.Lng, &j.Destination.Lat, &j.Destination.Lng,
			&j.DepartureTime, &j.EstimatedArrival, &j.VehicleType,
			&j.Status, &rejReason, &reservationID,
			&j.Version, &j.CreatedAt, &j.UpdatedAt,
			&j.CancelledAt, &j.ActivatedAt, &j.CompletedAt, &j.ExpiredAt,
		); err != nil {
			return nil, 0, apperrors.DatabaseError("failed to scan journey", err)
		}
		if rejReason.Valid {
			j.RejectionReason = rejReason.String
		}
		if reservationID.Valid {
			j.ReservationID = reservationID.String
		}
		journeys = append(journeys, j)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, apperrors.DatabaseError("rows error", err)
	}

	// Load segments for each journey
	for i := range journeys {
		segs, err := r.getSegments(ctx, journeys[i].JourneyID)
		if err != nil {
			return nil, 0, err
		}
		journeys[i].Segments = segs
	}

	return journeys, int64(len(journeys)), nil
}

// UpdateStatus updates a journey's status with optimistic locking
func (r *JourneyRepository) UpdateStatus(ctx context.Context, journeyID string, status model.JourneyStatus, version int, extra map[string]interface{}) error {
	setClauses := []string{
		"status = $1",
		"version = version + 1",
		"updated_at = $2",
	}
	args := []interface{}{string(status), time.Now()}

	for col, val := range extra {
		args = append(args, val)
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, len(args)))
	}

	args = append(args, journeyID, version)
	query := fmt.Sprintf(
		"UPDATE journey.journeys SET %s WHERE journey_id = $%d AND version = $%d",
		strings.Join(setClauses, ", "), len(args)-1, len(args),
	)

	res, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return apperrors.DatabaseError("failed to update journey status", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return apperrors.Conflict("journey was modified concurrently, please retry")
	}
	return nil
}

// HasActiveJourney returns true if the driver has an APPROVED or ACTIVE journey
func (r *JourneyRepository) HasActiveJourney(ctx context.Context, driverID string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM journey.journeys WHERE driver_id = $1 AND status IN ('APPROVED','ACTIVE')",
		driverID,
	).Scan(&count)
	if err != nil {
		return false, apperrors.DatabaseError("failed to check active journey", err)
	}
	return count > 0, nil
}

// SegmentAuthRecord holds the result of an enforcement segment lookup
type SegmentAuthRecord struct {
	JourneyID       string
	DriverID        string
	Status          string
	SegmentID       string
	TimeWindowStart time.Time
	TimeWindowEnd   time.Time
}

// FindActiveJourneyForSegment returns the ACTIVE journey covering segmentID at ts, or nil if none.
func (r *JourneyRepository) FindActiveJourneyForSegment(ctx context.Context, segmentID string, ts time.Time) (*SegmentAuthRecord, error) {
	var rec SegmentAuthRecord
	err := r.db.QueryRowContext(ctx, `
		SELECT j.journey_id, j.driver_id, j.status,
		       js.segment_id, js.time_window_start, js.time_window_end
		FROM journey.journeys j
		JOIN journey.journey_segments js ON j.journey_id = js.journey_id
		WHERE js.segment_id = $1
		  AND js.time_window_start <= $2
		  AND js.time_window_end >= $2
		  AND j.status = 'ACTIVE'
		LIMIT 1`, segmentID, ts).Scan(
		&rec.JourneyID, &rec.DriverID, &rec.Status,
		&rec.SegmentID, &rec.TimeWindowStart, &rec.TimeWindowEnd,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, apperrors.DatabaseError("failed to query enforcement", err)
	}
	return &rec, nil
}

// GetExpiredJourneys returns APPROVED journeys where departure_time < NOW() - 30min
func (r *JourneyRepository) GetExpiredJourneys(ctx context.Context) ([]model.Journey, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT journey_id, driver_id, version, reservation_id
		FROM journey.journeys
		WHERE status = 'APPROVED' AND departure_time < NOW() - INTERVAL '30 minutes'`)
	if err != nil {
		return nil, apperrors.DatabaseError("failed to get expired journeys", err)
	}
	defer rows.Close()

	var journeys []model.Journey
	for rows.Next() {
		var j model.Journey
		var reservationID sql.NullString
		if err := rows.Scan(&j.JourneyID, &j.DriverID, &j.Version, &reservationID); err != nil {
			return nil, apperrors.DatabaseError("failed to scan expired journey", err)
		}
		if reservationID.Valid {
			j.ReservationID = reservationID.String
		}
		journeys = append(journeys, j)
	}
	return journeys, rows.Err()
}

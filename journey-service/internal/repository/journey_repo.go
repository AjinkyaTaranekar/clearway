package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/model"
	apperrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/errors"
)

// JourneyRepository handles all database operations for journeys.
// Writes go to the master pool; reads use the slave pool (falls back to master
// if slave is nil, e.g. when running without replication in dev).
type JourneyRepository struct {
	db    *sql.DB // master — writes
	slave *sql.DB // slave  — reads (nil → fall back to master)
}

// NewJourneyRepository creates a new repository.
// slave may be nil; in that case all operations run against master.
func NewJourneyRepository(master, slave *sql.DB) *JourneyRepository {
	return &JourneyRepository{db: master, slave: slave}
}

// readDB returns the slave connection pool when available, otherwise master.
func (r *JourneyRepository) readDB() *sql.DB {
	if r.slave != nil {
		return r.slave
	}
	return r.db
}

// RunMigrations executes the SQL migration against the master database.
func (r *JourneyRepository) RunMigrations(ctx context.Context, sql string) error {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "JourneyRepository.RunMigrations").
		Int("sql_length", len(sql)).
		Msg("running journey migrations")

	_, err := r.db.ExecContext(ctx, sql)
	if err != nil {
		log.Error().
			Str("repository", "JourneyRepository.RunMigrations").
			Err(err).
			Msg("journey migration failed")
		return fmt.Errorf("migration failed: %w", err)
	}
	log.Info().Str("repository", "JourneyRepository.RunMigrations").Msg("journey migrations completed")
	return nil
}

// isPQUniqueViolation returns true when err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505). Uses structured error inspection rather than
// string matching so it works correctly across locales and driver versions.
func isPQUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

// Create inserts a new journey and its segments in a single transaction.
func (r *JourneyRepository) Create(ctx context.Context, j *model.Journey, segments []model.JourneySegment) error {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "JourneyRepository.Create").
		Str("journey_id", j.JourneyID).
		Str("driver_id", j.DriverID).
		Str("status", string(j.Status)).
		Int("segment_count", len(segments)).
		Msg("creating journey transaction")

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		log.Error().
			Str("repository", "JourneyRepository.Create").
			Err(err).
			Str("journey_id", j.JourneyID).
			Msg("failed to begin create journey transaction")
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
		if isPQUniqueViolation(err) {
			log.Warn().
				Str("repository", "JourneyRepository.Create").
				Str("journey_id", j.JourneyID).
				Str("idempotency_key", j.IdempotencyKey).
				Msg("journey create conflict due to unique constraint")
			return apperrors.Conflict("journey with this idempotency key already exists")
		}
		log.Error().
			Str("repository", "JourneyRepository.Create").
			Err(err).
			Str("journey_id", j.JourneyID).
			Msg("failed to insert journey row")
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
			log.Error().
				Str("repository", "JourneyRepository.Create").
				Err(err).
				Str("journey_id", j.JourneyID).
				Str("segment_id", seg.SegmentID).
				Msg("failed to insert journey segment")
			return apperrors.DatabaseError("failed to insert segment", err)
		}
	}

	if err := tx.Commit(); err != nil {
		log.Error().
			Str("repository", "JourneyRepository.Create").
			Err(err).
			Str("journey_id", j.JourneyID).
			Msg("failed to commit create journey transaction")
		return apperrors.DatabaseError("failed to commit create journey", err)
	}

	log.Info().
		Str("repository", "JourneyRepository.Create").
		Str("journey_id", j.JourneyID).
		Msg("journey transaction committed")
	return nil
}

// GetByID returns a journey and its segments by journey ID.
func (r *JourneyRepository) GetByID(ctx context.Context, journeyID string) (*model.Journey, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "JourneyRepository.GetByID").
		Str("journey_id", journeyID).
		Msg("querying journey by id")

	j := &model.Journey{}
	var rejReason, reservationID sql.NullString

	err := r.readDB().QueryRowContext(ctx, `
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
		log.Warn().
			Str("repository", "JourneyRepository.GetByID").
			Str("journey_id", journeyID).
			Msg("journey not found")
		return nil, apperrors.NotFound("journey not found")
	}
	if err != nil {
		log.Error().
			Str("repository", "JourneyRepository.GetByID").
			Err(err).
			Str("journey_id", journeyID).
			Msg("failed to query journey")
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
		log.Error().
			Str("repository", "JourneyRepository.GetByID").
			Err(err).
			Str("journey_id", journeyID).
			Msg("failed to load journey segments")
		return nil, err
	}
	j.Segments = segments
	log.Debug().
		Str("repository", "JourneyRepository.GetByID").
		Str("journey_id", journeyID).
		Int("segment_count", len(segments)).
		Msg("journey loaded by id")

	return j, nil
}

func (r *JourneyRepository) getSegments(ctx context.Context, journeyID string) ([]model.JourneySegment, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "JourneyRepository.getSegments").
		Str("journey_id", journeyID).
		Msg("loading journey segments")

	rows, err := r.readDB().QueryContext(ctx, `
		SELECT segment_id, segment_name, sequence_order,
		       time_window_start, time_window_end, traversal_minutes, region
		FROM journey.journey_segments
		WHERE journey_id = $1
		ORDER BY sequence_order`, journeyID)
	if err != nil {
		log.Error().
			Str("repository", "JourneyRepository.getSegments").
			Err(err).
			Str("journey_id", journeyID).
			Msg("failed to query journey segments")
		return nil, apperrors.DatabaseError("failed to get segments", err)
	}
	defer rows.Close()

	var segments []model.JourneySegment
	for rows.Next() {
		var s model.JourneySegment
		if err := rows.Scan(&s.SegmentID, &s.SegmentName, &s.SequenceOrder,
			&s.TimeWindowStart, &s.TimeWindowEnd, &s.TraversalMinutes, &s.Region); err != nil {
			log.Error().
				Str("repository", "JourneyRepository.getSegments").
				Err(err).
				Str("journey_id", journeyID).
				Msg("failed to scan journey segment")
			return nil, apperrors.DatabaseError("failed to scan segment", err)
		}
		segments = append(segments, s)
	}
	if err := rows.Err(); err != nil {
		log.Error().
			Str("repository", "JourneyRepository.getSegments").
			Err(err).
			Str("journey_id", journeyID).
			Msg("row iteration failed for journey segments")
		return nil, err
	}

	log.Debug().
		Str("repository", "JourneyRepository.getSegments").
		Str("journey_id", journeyID).
		Int("segment_count", len(segments)).
		Msg("journey segments loaded")
	return segments, nil
}

// ListByDriverID returns paginated journeys for a driver.
func (r *JourneyRepository) ListByDriverID(ctx context.Context, driverID, statusFilter string, page, limit int) ([]model.Journey, int64, error) {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "JourneyRepository.ListByDriverID").
		Str("driver_id", driverID).
		Str("status_filter", statusFilter).
		Int("page", page).
		Int("limit", limit).
		Msg("listing journeys for driver")

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
	err := r.readDB().QueryRowContext(ctx, "SELECT COUNT(*) FROM journey.journeys "+where, args...).Scan(&total)
	if err != nil {
		log.Error().
			Str("repository", "JourneyRepository.ListByDriverID").
			Err(err).
			Str("driver_id", driverID).
			Msg("failed to count driver journeys")
		return nil, 0, apperrors.DatabaseError("failed to count journeys", err)
	}

	args = append(args, limit, offset)
	rows, err := r.readDB().QueryContext(ctx, `
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
		log.Error().
			Str("repository", "JourneyRepository.ListByDriverID").
			Err(err).
			Str("driver_id", driverID).
			Msg("failed to query driver journeys")
		return nil, 0, apperrors.DatabaseError("failed to list journeys", err)
	}
	defer rows.Close()

	journeys, countedTotal, err := r.scanJourneys(ctx, rows, total)
	if err != nil {
		log.Error().
			Str("repository", "JourneyRepository.ListByDriverID").
			Err(err).
			Str("driver_id", driverID).
			Msg("failed to scan driver journey rows")
		return nil, 0, err
	}

	log.Info().
		Str("repository", "JourneyRepository.ListByDriverID").
		Str("driver_id", driverID).
		Int("result_count", len(journeys)).
		Int64("total", countedTotal).
		Msg("listed journeys for driver")
	return journeys, countedTotal, nil
}

// AdminList returns all journeys with optional filters.
func (r *JourneyRepository) AdminList(ctx context.Context, statusFilter, driverIDFilter string, page, limit int) ([]model.Journey, int64, error) {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "JourneyRepository.AdminList").
		Str("status_filter", statusFilter).
		Str("driver_id_filter", driverIDFilter).
		Int("page", page).
		Int("limit", limit).
		Msg("listing journeys for admin")

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
	err := r.readDB().QueryRowContext(ctx, "SELECT COUNT(*) FROM journey.journeys "+where, args...).Scan(&total)
	if err != nil {
		log.Error().
			Str("repository", "JourneyRepository.AdminList").
			Err(err).
			Msg("failed to count admin journey listing")
		return nil, 0, apperrors.DatabaseError("failed to count journeys", err)
	}

	args = append(args, limit, offset)
	rows, err := r.readDB().QueryContext(ctx, `
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
		log.Error().
			Str("repository", "JourneyRepository.AdminList").
			Err(err).
			Msg("failed to query admin journey listing")
		return nil, 0, apperrors.DatabaseError("failed to admin list journeys", err)
	}
	defer rows.Close()

	journeys, countedTotal, err := r.scanJourneys(ctx, rows, total)
	if err != nil {
		log.Error().
			Str("repository", "JourneyRepository.AdminList").
			Err(err).
			Msg("failed to scan admin journey listing rows")
		return nil, 0, err
	}

	log.Info().
		Str("repository", "JourneyRepository.AdminList").
		Int("result_count", len(journeys)).
		Int64("total", countedTotal).
		Msg("admin journey listing completed")
	return journeys, countedTotal, nil
}

// scanJourneys scans a result set of journey rows and loads their segments.
// The total parameter is the DB-level COUNT(*) already computed by the caller;
// it is passed through unchanged so the API response reflects the real total
// rather than the page length.
func (r *JourneyRepository) scanJourneys(ctx context.Context, rows *sql.Rows, total int64) ([]model.Journey, int64, error) {
	log := logWithTrace(ctx)
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
			log.Error().
				Str("repository", "JourneyRepository.scanJourneys").
				Err(err).
				Msg("failed to scan journey row")
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
		log.Error().
			Str("repository", "JourneyRepository.scanJourneys").
			Err(err).
			Msg("row iteration failed while scanning journeys")
		return nil, 0, apperrors.DatabaseError("rows error", err)
	}

	for i := range journeys {
		segs, err := r.getSegments(ctx, journeys[i].JourneyID)
		if err != nil {
			log.Error().
				Str("repository", "JourneyRepository.scanJourneys").
				Err(err).
				Str("journey_id", journeys[i].JourneyID).
				Msg("failed to load segments while scanning journeys")
			return nil, 0, err
		}
		journeys[i].Segments = segs
	}

	log.Debug().
		Str("repository", "JourneyRepository.scanJourneys").
		Int("result_count", len(journeys)).
		Int64("total", total).
		Msg("journey rows scanned")

	return journeys, total, nil
}

// UpdateStatus updates a journey's status with optimistic locking (master).
func (r *JourneyRepository) UpdateStatus(ctx context.Context, journeyID string, status model.JourneyStatus, version int, extra map[string]interface{}) error {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "JourneyRepository.UpdateStatus").
		Str("journey_id", journeyID).
		Str("new_status", string(status)).
		Int("expected_version", version).
		Int("extra_field_count", len(extra)).
		Msg("updating journey status")

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
		log.Error().
			Str("repository", "JourneyRepository.UpdateStatus").
			Err(err).
			Str("journey_id", journeyID).
			Msg("failed to update journey status")
		return apperrors.DatabaseError("failed to update journey status", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		log.Warn().
			Str("repository", "JourneyRepository.UpdateStatus").
			Str("journey_id", journeyID).
			Int("expected_version", version).
			Msg("journey status update conflict")
		return apperrors.Conflict("journey was modified concurrently, please retry")
	}
	log.Info().
		Str("repository", "JourneyRepository.UpdateStatus").
		Str("journey_id", journeyID).
		Str("new_status", string(status)).
		Msg("journey status updated")
	return nil
}

// HasActiveJourney returns true if the driver has an APPROVED or ACTIVE journey.
func (r *JourneyRepository) HasActiveJourney(ctx context.Context, driverID string) (bool, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "JourneyRepository.HasActiveJourney").
		Str("driver_id", driverID).
		Msg("checking active journey existence")

	var count int
	err := r.readDB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM journey.journeys WHERE driver_id = $1 AND status IN ('APPROVED','ACTIVE')",
		driverID,
	).Scan(&count)
	if err != nil {
		log.Error().
			Str("repository", "JourneyRepository.HasActiveJourney").
			Err(err).
			Str("driver_id", driverID).
			Msg("failed to check active journey existence")
		return false, apperrors.DatabaseError("failed to check active journey", err)
	}
	log.Debug().
		Str("repository", "JourneyRepository.HasActiveJourney").
		Str("driver_id", driverID).
		Int("active_count", count).
		Bool("has_active", count > 0).
		Msg("checked active journey existence")
	return count > 0, nil
}

// SegmentAuthRecord holds the result of an enforcement segment lookup.
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
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "JourneyRepository.FindActiveJourneyForSegment").
		Str("segment_id", segmentID).
		Time("timestamp", ts).
		Msg("querying active journey for segment")

	var rec SegmentAuthRecord
	err := r.readDB().QueryRowContext(ctx, `
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
		log.Info().
			Str("repository", "JourneyRepository.FindActiveJourneyForSegment").
			Str("segment_id", segmentID).
			Msg("no active journey found for segment")
		return nil, nil
	}
	if err != nil {
		log.Error().
			Str("repository", "JourneyRepository.FindActiveJourneyForSegment").
			Err(err).
			Str("segment_id", segmentID).
			Msg("failed to query enforcement record")
		return nil, apperrors.DatabaseError("failed to query enforcement", err)
	}
	log.Info().
		Str("repository", "JourneyRepository.FindActiveJourneyForSegment").
		Str("segment_id", segmentID).
		Str("journey_id", rec.JourneyID).
		Str("driver_id", rec.DriverID).
		Msg("active journey found for segment")
	return &rec, nil
}

// GetExpiredJourneys returns APPROVED journeys where departure_time < NOW() - 30min.
func (r *JourneyRepository) GetExpiredJourneys(ctx context.Context) ([]model.Journey, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "JourneyRepository.GetExpiredJourneys").
		Msg("querying expired approved journeys")

	rows, err := r.readDB().QueryContext(ctx, `
		SELECT journey_id, driver_id, version, reservation_id
		FROM journey.journeys
		WHERE status = 'APPROVED' AND departure_time < NOW() - INTERVAL '30 minutes'`)
	if err != nil {
		log.Error().
			Str("repository", "JourneyRepository.GetExpiredJourneys").
			Err(err).
			Msg("failed to query expired journeys")
		return nil, apperrors.DatabaseError("failed to get expired journeys", err)
	}
	defer rows.Close()

	var journeys []model.Journey
	for rows.Next() {
		var j model.Journey
		var reservationID sql.NullString
		if err := rows.Scan(&j.JourneyID, &j.DriverID, &j.Version, &reservationID); err != nil {
			log.Error().
				Str("repository", "JourneyRepository.GetExpiredJourneys").
				Err(err).
				Msg("failed to scan expired journey row")
			return nil, apperrors.DatabaseError("failed to scan expired journey", err)
		}
		if reservationID.Valid {
			j.ReservationID = reservationID.String
		}
		journeys = append(journeys, j)
	}
	if err := rows.Err(); err != nil {
		log.Error().
			Str("repository", "JourneyRepository.GetExpiredJourneys").
			Err(err).
			Msg("row iteration failed for expired journeys")
		return nil, err
	}

	log.Debug().
		Str("repository", "JourneyRepository.GetExpiredJourneys").
		Int("expired_count", len(journeys)).
		Msg("expired journeys loaded")
	return journeys, nil
}

package bo_v1_models

import (
	"context"
	commonModels "lexicon/bo-api/common/models"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog/log"
	"gopkg.in/guregu/null.v4"
)

type SearchResultModel struct {
	ID                   ulid.ULID   `json:"id"`
	Subject              string      `json:"subject"`
	SubjectType          string      `json:"subject_type"`
	PersonInCharge       null.String `json:"person_in_charge"`
	BenificiaryOwnership null.String `json:"benificiary_ownership"`
	Nation               string      `json:"nation"`
	Type                 string      `json:"type"`
	Year                 string      `json:"year"`
}

type tempSearchResult struct {
	ID                   ulid.ULID
	Subject              string
	SubjectType          SubjectTypeInt
	PersonInCharge       null.String
	BenificiaryOwnership null.String
	Nation               string
	Type                 CaseType
	Year                 string
}

var emptyBaseModel commonModels.BasePaginationResponse

func SearchByRequest(ctx context.Context, tx pgx.Tx, searchRequest SearchRequest) (commonModels.BasePaginationResponse, error) {
	var itemCount int

	limit := 20
	offset := (int(searchRequest.Page) - 1) * limit
	log.Info().Msg("Start counting query")
	countQuery := `
	SELECT COUNT(id) as cnt
	FROM cases
	`
	if searchRequest.Query != "" {
		countQuery += "WHERE fulltext_search_index @@ phraseto_tsquery('english',$1)"
	} else {
		countQuery += "WHERE $1 = $1"
	}

	countQuery += `
	AND subject_type = ANY($2::int[])
	AND year ~* $3
	AND case_type = ANY($4::int[])
	AND nation ~* $5
	AND status = $6
	`
	log.Info().Msg("Executing query: " + countQuery)

	row := tx.QueryRow(ctx, countQuery, searchRequest.Query, normalizeSubjectTypes(searchRequest.SubjectTypes), normalizeYears(searchRequest.Years), normalizeCaseTypes(searchRequest.Types), normalizeNations(searchRequest.Nations), validated)
	err := row.Scan(&itemCount)
	log.Info().Msg("Finish counting query")

	if err != nil {
		return emptyBaseModel, err
	}

	if itemCount == 0 {
		return emptyBaseModel, nil
	}

	log.Info().Msg("Start searching query")
	searchQuery := `SELECT id, subject, subject_type, person_in_charge, benificiary_ownership, nation, case_type, year`

	if searchRequest.Query != "" {
		searchQuery += ", ts_rank_cd(fulltext_search_index, phraseto_tsquery('english', $1), 32 /* rank/(rank+1) */ ) AS rank "
	} else {
		searchQuery += ", 0 AS rank "
	}

	searchQuery += " FROM cases "

	if searchRequest.Query != "" {
		searchQuery += "WHERE fulltext_search_index @@ phraseto_tsquery('english', $1)"
	} else {
		searchQuery += "WHERE $1 = $1 "
	}

	searchQuery += `
	AND subject_type = ANY($2::int[])
	AND year ~* $3
	AND case_type = ANY($4::int[])
	AND nation ~* $5
	AND status = $6
	`

	if searchRequest.Query != "" {
		searchQuery += "ORDER BY rank DESC"
	}

	searchQuery += " LIMIT $7 OFFSET $8 "

	log.Info().Msg("Executing query: " + searchQuery)

	rows, err := tx.Query(ctx, searchQuery, searchRequest.Query, normalizeSubjectTypes(searchRequest.SubjectTypes), normalizeYears(searchRequest.Years), normalizeCaseTypes(searchRequest.Types), normalizeNations(searchRequest.Nations), validated, limit, offset)

	log.Info().Msg("Finish searching query")
	if err != nil {
		log.Error().Err(err).Msg("Error querying database")
		return emptyBaseModel, err
	}

	defer rows.Close()

	var searchResults []SearchResultModel

	for rows.Next() {
		var rank float64
		var tempResult tempSearchResult
		err = rows.Scan(&tempResult.ID, &tempResult.Subject, &tempResult.SubjectType, &tempResult.PersonInCharge, &tempResult.BenificiaryOwnership, &tempResult.Nation, &tempResult.Type, &tempResult.Year, &rank)

		if err != nil {
			return emptyBaseModel, err
		}
		// map to search result
		searchResult := SearchResultModel{
			ID:                   tempResult.ID,
			Subject:              tempResult.Subject,
			SubjectType:          tempResult.SubjectType.String(),
			PersonInCharge:       tempResult.PersonInCharge,
			BenificiaryOwnership: tempResult.BenificiaryOwnership,
			Nation:               tempResult.Nation,
			Type:                 tempResult.Type.String(),
			Year:                 tempResult.Year,
		}

		searchResults = append(searchResults, searchResult)
	}

	if len(searchResults) == 0 {
		return emptyBaseModel, nil
	}

	metaResponse := commonModels.MetaResponse{
		CurrentPage: searchRequest.Page,
		LastPage:    int64(math.Ceil(float64(itemCount) / float64(limit))),
		PerPage:     int64(limit),
		Total:       int64(itemCount),
	}

	baseResponse := commonModels.BasePaginationResponse{
		Data: searchResults,
		Meta: metaResponse,
	}

	return baseResponse, nil
}

func normalizeYears(years []string) string {
	return strings.Join(years, "|")
}

func normalizeCaseTypes(caseTypes []string) pgtype.FlatArray[CaseType] {

	var tempCaseTypes pgtype.FlatArray[CaseType]

	if len(caseTypes) == 0 {
		caseTypes = []string{verdict.String(), blacklist.String(), sanction.String()}
	}
	for _, caseType := range caseTypes {

		tempCaseTypes = append(tempCaseTypes, newCaseType(caseType))
	}
	return tempCaseTypes
}

func normalizeNations(nations []string) string {
	return strings.Join(nations, "|")
}

func normalizeSubjectTypes(subjectTypes []string) pgtype.FlatArray[SubjectTypeInt] {
	var tempSubjectTypes pgtype.FlatArray[SubjectTypeInt]

	if len(subjectTypes) == 0 {
		subjectTypes = []string{individual.String(), company.String(), organization.String()}
	}

	for _, subjectType := range subjectTypes {
		tempSubjectTypes = append(tempSubjectTypes, newSubjectType(subjectType))

	}
	return tempSubjectTypes
}

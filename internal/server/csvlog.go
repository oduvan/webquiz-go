package server

// The CSV files of a quiz run. Task 7 builds them (server.py
// generate_csv_path, flush_responses_to_csv, flush_users_to_csv and
// _append_answers_with_users_csv).

// startCSVFilesLocked chooses the CSV files of a new run of the quiz file
// quizFilename: s.csvFile, s.userCSVFile and, with
// extra_answers_with_users_csv, s.answersWithUsersCSVFile (generate_csv_path:
// "<quiz>_NNNN.csv" with the same free number for all three). SwitchQuiz
// calls it; the caller holds mu.
func (s *Server) startCSVFilesLocked(quizFilename string) {
	_ = quizFilename
	s.csvFile = ""
	s.userCSVFile = ""
	s.answersWithUsersCSVFile = ""
}

// flushResponsesToCSV writes the answers waiting in s.userResponses to the
// answers CSV (and the answers_with_users CSV). The flush loop calls it
// every s.flushInterval (5 s) and Close calls it once more.
func (s *Server) flushResponsesToCSV() error {
	return nil
}

// flushUsersToCSV writes every student with statistics to the users CSV.
func (s *Server) flushUsersToCSV() error {
	return nil
}

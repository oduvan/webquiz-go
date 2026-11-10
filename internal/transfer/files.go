// Package transfer sends files between WebQuiz servers that are connected
// to the same SSH tunnel server. It is a translation of transfer.py from
// WebQuiz 1.19, and talks to Python 1.19 servers in the same JSON.
//
// Servers connected to the same tunnel server (one SSH user, one socket
// folder) reach each other at "{base_url}/{socket name}/". The transfer is
// "ask first, then send":
//
//  1. Sender: the admin selects files and types the receiver's name. The
//     sender keeps an outgoing offer with a random token and POSTs the file
//     list and the token to the receiver (POST /api/transfer/offer).
//  2. Receiver: the request is shown to the admin, who accepts or rejects it.
//  3. On accept, the receiver downloads every file from the sender
//     (GET /api/transfer/download/{token}/{type}/{filename}) and reports the
//     result to the sender (POST /api/transfer/result).
//
// The receiver downloads only from "{its own base_url}/{sender name}/", so
// files can only come from a server connected to the same tunnel server. The
// token protects the sender: only offered files are served, only with the
// token, and only until the offer is finished or expired.
//
// Images (quizzes/imgs/) and attachments (quizzes/attach/) used by an offered
// quiz are added to the offer automatically.
//
// For every incoming file the receiver shows its case (new file, the same
// file already exists, a different file exists, or the file is used by the
// running server) and the admin chooses an action: save, replace, rename or
// skip. Images and attachments are saved first; if one gets a new name, the
// references in the received quiz are updated.
package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/oduvan/webquiz-go/internal/explanations"
	"github.com/oduvan/webquiz-go/internal/pyval"
)

// fileTypes are the file types that can be transferred, with their allowed
// extensions (nil: any extension).
var fileTypes = map[string][]string{
	"quizzes":     {".yaml", ".yml"},
	"logs":        {".log"},
	"csv":         {".csv"},
	"images":      {".png", ".jpg", ".jpeg", ".gif", ".bmp", ".svg", ".webp"},
	"attachments": nil,
}

// Files used by quizzes: sent together with the quizzes that reference them.
const (
	imagesURLPrefix      = "/imgs/"
	attachmentsURLPrefix = "/attach/"
)

func isMediaType(fileType string) bool { return fileType == "images" || fileType == "attachments" }

// Logs and CSV files have the same names on every server, so they get the
// sender's name.
func isPrefixedType(fileType string) bool { return fileType == "logs" || fileType == "csv" }

// The patterns of transfer.py. Python's "$" also matches before a newline
// at the end of the text, so "\n?$" here.
var (
	// Socket names become a URL path segment and a file name prefix.
	serverNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}\n?$`)
	tokenPattern      = regexp.MustCompile(`^[A-Za-z0-9_-]{40,100}\n?$`)
	sha256Pattern     = regexp.MustCompile(`^[0-9a-f]{64}\n?$`)
)

// isValidServerName checks that a name can be used as a socket name in URLs
// and file names (is_valid_server_name).
func isValidServerName(name any) bool {
	s, ok := name.(string)
	return ok && serverNamePattern.MatchString(s) && !strings.Contains(s, "..")
}

// isAllowedFile checks that a file of this type and name may be sent or
// received (is_allowed_file). Only plain file names (no paths, no hidden
// files, no characters that Windows does not allow) with the extension
// allowed for the type are accepted.
func isAllowedFile(fileType, filename any) bool {
	kind, ok := fileType.(string)
	if !ok {
		return false
	}
	extensions, known := fileTypes[kind]
	name, isText := filename.(string)
	if !known || !isText {
		return false
	}
	if name == "" || utf8.RuneCountInString(name) > 255 || strings.HasPrefix(name, ".") {
		return false
	}
	for _, char := range name {
		if char < 32 || strings.ContainsRune(`<>:"/\|?*`, char) {
			return false
		}
	}
	if extensions == nil {
		return true
	}
	lower := strings.ToLower(name)
	for _, extension := range extensions {
		if strings.HasSuffix(lower, extension) {
			return true
		}
	}
	return false
}

// fileSHA256 is the SHA-256 of a file's content as hex.
func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// mediaFile is an image or attachment: (file type, file name).
type mediaFile struct {
	Type string
	Name string
}

// mediaFileName is the file name of the image or attachment that a quiz
// value points to (_media_file_name). Images are referenced as
// "/imgs/name.png" (in "image" and in "options"), attachments as "name.pdf"
// or "/attach/name.pdf" (in "file").
func mediaFileName(fileType string, value any) (string, bool) {
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	var name string
	if fileType == "images" {
		if !strings.HasPrefix(text, imagesURLPrefix) {
			return "", false
		}
		name = text[len(imagesURLPrefix):]
	} else {
		name = strings.TrimPrefix(text, attachmentsURLPrefix)
	}
	return name, isAllowedFile(fileType, name)
}

// quizMediaFiles lists the images and attachments a quiz file uses, without
// duplicates (quiz_media_files). A file that cannot be read gives none.
func quizMediaFiles(quizPath string) []mediaFile {
	data, err := os.ReadFile(quizPath)
	if err != nil || !utf8.Valid(data) {
		return nil
	}
	quiz, err := pyval.LoadYAML(data)
	if err != nil {
		return nil
	}
	var found []mediaFile
	add := func(f mediaFile) {
		for _, seen := range found {
			if seen == f {
				return
			}
		}
		found = append(found, f)
	}
	questions := quizQuestions(quiz)
	for _, question := range questions {
		if value, ok := question["image"]; ok {
			if name, ok := mediaFileName("images", value); ok {
				add(mediaFile{"images", name})
			}
		}
		if value, ok := question["file"]; ok {
			if name, ok := mediaFileName("attachments", value); ok {
				add(mediaFile{"attachments", name})
			}
		}
		if options, ok := question["options"].([]any); ok {
			for _, option := range options {
				if name, ok := mediaFileName("images", option); ok {
					add(mediaFile{"images", name})
				}
			}
		}
	}
	// Explanations may show quiz images too.
	for _, question := range questions {
		text, ok := question["explanation"].(string)
		if !ok {
			continue
		}
		for _, name := range explanations.ImageNames(text) {
			if isAllowedFile("images", name) {
				add(mediaFile{"images", name})
			}
		}
	}
	return found
}

// quizQuestions are the questions of quiz data that are mappings.
func quizQuestions(quiz any) []map[string]any {
	data, ok := quiz.(map[string]any)
	if !ok {
		return nil
	}
	list, ok := data["questions"].([]any)
	if !ok {
		return nil
	}
	var questions []map[string]any
	for _, item := range list {
		if question, ok := item.(map[string]any); ok {
			questions = append(questions, question)
		}
	}
	return questions
}

// splitExt is Python's os.path.splitext for a file name without folders:
// the extension starts at the last dot, but dots at the start do not count.
func splitExt(name string) (stem, extension string) {
	dot := strings.LastIndex(name, ".")
	if dot <= 0 || strings.Trim(name[:dot], ".") == "" {
		return name, ""
	}
	return name[:dot], name[dot:]
}

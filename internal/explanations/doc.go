// Package explanations handles question explanations: Markdown text that a
// student sees after answering a question.
//
// It is a port of webquiz/explanations.py. Render turns the Markdown into
// HTML that is safe to put into a page, with the same rules as the Python
// version (which used mistune):
//
//   - raw HTML in the Markdown is shown as text, never run;
//   - links with harmful schemes (javascript: and anything that is not
//     http(s), mailto, tel, ftp(s), irc(s), a relative address or a data:
//     image) are shown as plain text;
//   - other links open in a new tab (target="_blank" rel="noopener");
//   - tables and ~~strikethrough~~ work, as with mistune's plugins.
//
// Images use the same path form as question images, "/imgs/name.png". Render
// turns that into a path for the page that shows it: StudentImagesURL for the
// quiz page (served at "/"), AdminImagesURL for the admin preview (served at
// "/admin/").
//
// Has tells whether a question has explanation text, ImageNames lists the
// quiz images an explanation shows, RewriteImages points them to new file
// names (used by file transfer), and Validate checks explanations before a
// quiz is saved.
package explanations

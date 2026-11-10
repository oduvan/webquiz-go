package main

// usageLine is shown before an error message about the options.
const usageLine = "usage: webquiz [-h] [-d] [--stop] [--status] [--config CONFIG] [--port PORT]\n" +
	"               [--quizzes-dir QUIZZES_DIR] [--master-key MASTER_KEY]\n" +
	"               [--logs-dir LOGS_DIR] [--csv-dir CSV_DIR] [--static STATIC]\n" +
	"               [--url-format URL_FORMAT] [--version]\n"

// helpText is the text of --help: the argparse help of cli.py, with the same
// examples.
const helpText = usageLine + `
WebQuiz - A modern web-based quiz and testing platform

options:
  -h, --help                  show this help message and exit
  -d, --daemon                Run server as daemon in background
  --stop                      Stop daemon server
  --status                    Check daemon status
  --config CONFIG             Path to YAML configuration file
  --port PORT                 Port to listen on (default: 8080)
  --quizzes-dir QUIZZES_DIR   Path to quizzes directory (default: quizzes)
  --master-key MASTER_KEY     Master key for admin access (can also use
                              WEBQUIZ_MASTER_KEY env var)
  --logs-dir LOGS_DIR         Directory for log files (default: logs/)
  --csv-dir CSV_DIR           Directory for CSV response files (default: data/)
  --static STATIC             Path to static files directory (default: static)
  --url-format URL_FORMAT     URL format for admin panel (default:
                              http://{IP}:{PORT}/)
  --version                   show the program's version number and exit

Examples:
  webquiz                              Start server with defaults
  webquiz -d                           Start server as daemon
  webquiz --config server.yaml        Use configuration file
  webquiz --port 9090                  Listen on a custom port
  webquiz --quizzes-dir my_quizzes     Use custom quizzes directory
  webquiz --master-key secret123       Set admin master key
  webquiz --logs-dir /var/log          Use custom logs directory
  webquiz --csv-dir /data              Use custom CSV directory
  webquiz --static /var/www/quiz       Use custom static files directory
  WEBQUIZ_MASTER_KEY=secret webquiz    Set master key via environment
  webquiz --config prod.yaml --master-key override  Mix config file with CLI overrides
  webquiz --stop                       Stop daemon server
  webquiz --status                     Check daemon status

The server will be available at http://0.0.0.0:8080 (accessible on network)
Quiz files loaded from quizzes/ directory (auto-created if missing)
Admin panel available at http://0.0.0.0:8080/admin (if master key is set)
User responses saved to data/{quiz_name}_user_responses_{suffix}.csv
Server logs written to logs/server_{suffix}.log
Static files served from static/ directory
CLI parameters always override config file values
`

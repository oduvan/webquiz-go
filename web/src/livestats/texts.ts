// The texts of the live stats page. Python's templates/live_stats.html had them
// in English only (translations.py has no live stats texts), so they are kept here
// as they were.
export const TEXTS = {
  toggleTheme: 'Toggle theme',
  filterAll: 'All',
  filterInProgress: '🔄 In Progress',
  filterCompleted: '✅ Completed',
  userHeader: 'User',
  waitingForUsers: 'Waiting for users to join the quiz...',
  noUsersInCategory: 'No users in this category yet.',
  disconnected: '🔴 Disconnected from server',
  connectionError: '🔴 Connection error',
  failedToConnect: '🔴 Failed to connect',
  failedToReconnect: '🔴 Failed to reconnect. Refresh page to try again.',
  reconnecting: (seconds: number, attempt: number, max: number) =>
    '🔄 Reconnecting in ' + seconds + 's... (' + attempt + '/' + max + ')',
  questionLabel: (number: number) => 'Q' + number,
  correctTitle: (name: string, stats: string) => name + ' - Correct: ' + stats,
  pointsTitle: (points: string) => points + ' points',
};

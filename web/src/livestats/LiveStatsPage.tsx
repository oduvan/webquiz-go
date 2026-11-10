import { useSyncExternalStore } from 'react';
import { formatPoints } from '../shared/points';
import { plain } from '../shared/text';
import type { Filter, LiveStatsClient, LiveView } from './client';
import {
  formatTime,
  groupUsers,
  imageFilename,
  pointsOf,
  questionStatistics,
  stateIcon,
  userStatistics,
  type LiveQuestion,
  type QuestionStatistics,
} from './stats';
import { TEXTS } from './texts';

function FilterButton({ id, countId, label, count, active, onClick }: { id: string; countId: string; label: string; count: number; active: boolean; onClick: () => void }) {
  return (
    <button type="button" id={id} className={'filter-button' + (active ? ' active' : '')} onClick={onClick}>
      <span>{label}</span>
      <span className="filter-badge" id={countId}>
        {count}
      </span>
    </button>
  );
}

function QuestionHeader({ question, index, stats }: { question: LiveQuestion; index: number; stats: QuestionStatistics }) {
  const points = pointsOf(question);
  const statsText = stats.correct_count + '/' + stats.total_attempts;
  const fileIndicator = question.file ? (
    <span className="file-indicator" title={plain(question.file).split('/').pop()}>
      📎
    </span>
  ) : null;
  const pointsIndicator =
    points !== 1 ? (
      <span className="points-indicator" title={TEXTS.pointsTitle(formatPoints(points))}>
        🏆 {formatPoints(points)}
      </span>
    ) : null;

  if (question.image && !question.question) {
    // An image-only question: a small image and the file name
    const name = imageFilename(question.image);
    const title = TEXTS.correctTitle(name, statsText) + (points !== 1 ? ' - ' + TEXTS.pointsTitle(formatPoints(points)) : '');
    return (
      <th title={title}>
        <div className="image-header">
          <img src={plain(question.image)} alt={name} className="header-image" />
          <div className="image-filename">
            {name}
            {fileIndicator}
            {pointsIndicator}
          </div>
          <div className="question-stats">{statsText}</div>
        </div>
      </th>
    );
  }

  const text = question.question ? plain(question.question) : TEXTS.questionLabel(index + 1);
  const title = TEXTS.correctTitle(text, statsText) + (points > 1 ? ' - ' + TEXTS.pointsTitle(String(points)) : '');
  return (
    <th title={title}>
      <div className="question-header">
        <div className="question-text">
          {text}
          {fileIndicator}
          {pointsIndicator}
        </div>
        <div className="question-stats">{statsText}</div>
      </div>
    </th>
  );
}

function StatsTable({ view, userIds }: { view: LiveView; userIds: string[] }) {
  const userStats = userStatistics(view.liveStats, view.questions);
  const questionStats = questionStatistics(view.liveStats, view.questions);
  const questionIds = view.questions.map((_question, index) => index + 1);
  return (
    <table className="stats-table" id="statsTable">
      <thead>
        <tr id="statsHeader">
          <th className="username-header">{TEXTS.userHeader}</th>
          {view.questions.map((question, index) => (
            <QuestionHeader
              key={index}
              question={question}
              index={index}
              stats={questionStats[index + 1] || { correct_count: 0, total_attempts: 0 }}
            />
          ))}
        </tr>
      </thead>
      <tbody id="statsBody">
        {userIds.map((userId) => {
          const stats = userStats[userId] || { earned_points: 0 };
          return (
            <tr key={userId} id={'user-' + userId}>
              <td className="username-cell">
                <div className="username-content">
                  <div className="username-text">{plain(view.users[userId])}</div>
                  <span className="user-stats">
                    {formatPoints(stats.earned_points)}/{formatPoints(view.totalPoints)}
                  </span>
                </div>
              </td>
              {questionIds.map((questionId) => {
                const cells = view.liveStats[userId];
                const cell = cells && cells[questionId] ? cells[questionId] : { state: 'empty', time_taken: null };
                const state = cell.state || 'empty';
                const time = formatTime(cell.time_taken);
                return (
                  <td key={questionId} className={'state-cell state-' + state} id={'cell-' + userId + '-' + questionId}>
                    <div className="cell-content">
                      <div className="state-icon">{stateIcon(state)}</div>
                      {time ? <div className="time-display">{time}</div> : null}
                    </div>
                  </td>
                );
              })}
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

export function LiveStatsPage({ client }: { client: LiveStatsClient }) {
  const view = useSyncExternalStore(client.subscribe, client.getSnapshot);
  const groups = groupUsers(view.users, view.completedUsers, view.completionTimes);
  const shown: Record<Filter, string[]> = { all: groups.all, inProgress: groups.inProgress, completed: groups.completed };
  const userIds = shown[view.filter];
  const noData = groups.all.length === 0 || userIds.length === 0;

  return (
    <div className="live-page">
      <div
        id="connectionStatus"
        className={'connection-status ' + view.status.kind}
        style={{ display: view.status.visible ? 'block' : 'none' }}
      >
        {view.status.text}
      </div>
      <div className="filter-bar">
        <FilterButton id="filterAll" countId="countAll" label={TEXTS.filterAll} count={groups.all.length} active={view.filter === 'all'} onClick={() => client.setFilter('all')} />
        <FilterButton
          id="filterInProgress"
          countId="countInProgress"
          label={TEXTS.filterInProgress}
          count={groups.inProgress.length}
          active={view.filter === 'inProgress'}
          onClick={() => client.setFilter('inProgress')}
        />
        <FilterButton
          id="filterCompleted"
          countId="countCompleted"
          label={TEXTS.filterCompleted}
          count={groups.completed.length}
          active={view.filter === 'completed'}
          onClick={() => client.setFilter('completed')}
        />
      </div>
      <div className="stats-container">
        {noData ? (
          <div id="noDataMessage" className="no-data">
            {groups.all.length === 0 ? TEXTS.waitingForUsers : TEXTS.noUsersInCategory}
          </div>
        ) : (
          <div id="statsSection" className="group-section">
            <div className="table-wrapper">
              <StatsTable view={view} userIds={userIds} />
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

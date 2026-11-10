// The student page texts of WebQuiz 1.19, copied from the Python source
// (webquiz/translations.py, TRANSLATIONS["uk"] and TRANSLATIONS["en"]).
//
// The matrix checks compare the screen with these texts, so they must stay
// exactly as Python had them. Only the keys the checks use are here.

export type Language = 'uk' | 'en';

export interface StudentTexts {
  register_title: string;
  register_button: string;
  username_label: string;
  waiting_title: string;
  waiting_description: string;
  waiting_hint: string;
  save_changes: string;
  check_status: string;
  not_approved_yet: string;
  question_n_of_m: string;
  submit_answer: string;
  continue_button: string;
  explanation_title: string;
  next_button: string;
  enter_answer_label: string;
  enter_answer_placeholder: string;
  multiple_choice_hint: string;
  points_label: string;
  points_2_4: string;
  points_other: string;
  time_left: string;
  time_is_up: string;
  correct_answer_label: string;
  your_answer_label: string;
  also_correct_label: string;
  correct_answer_value: string;
  result_label: string;
  result_correct_header: string;
  question_header: string;
  your_answer_header: string;
  correct_header_alt: string;
  points_header: string;
  answers_available_later: string;
  reload_later: string;
  reload_page: string;
}

// webquiz/translations.py lines 4-102 ("uk") and 103-201 ("en")
export const TEXTS: Record<Language, StudentTexts> = {
  uk: {
    register_title: 'Зареєструйтеся для початку тесту',
    register_button: 'Зареєструватися',
    username_label: "Ім'я користувача",
    waiting_title: 'Очікування підтвердження',
    waiting_description: 'Ваша реєстрація очікує підтвердження адміністратором.',
    waiting_hint: 'Ви можете змінити свої дані нижче та натиснути кнопку "Перевірити", щоб дізнатися чи вас підтвердили.',
    save_changes: '💾 Зберегти зміни',
    check_status: '🔄 Перевірити',
    not_approved_yet: 'Ви ще не підтверджені. Будь ласка, зачекайте.',
    question_n_of_m: 'Питання {n} з {m}',
    submit_answer: 'Відправити відповідь',
    continue_button: 'Продовжити',
    explanation_title: '📖 Пояснення',
    next_button: 'Далі',
    enter_answer_label: '✍️ Введіть вашу відповідь:',
    enter_answer_placeholder: 'Введіть відповідь...',
    multiple_choice_hint: '📋 Можна обрати кілька відповідей',
    points_label: 'Бали:',
    points_2_4: 'бали',
    points_other: 'балів',
    time_left: 'Залишилось часу',
    time_is_up: 'Час вичерпано',
    correct_answer_label: '✓ Правильна відповідь',
    your_answer_label: '✗ Ваша відповідь',
    also_correct_label: '✓ Теж правильна відповідь',
    correct_answer_value: '✓ Правильна відповідь: ',
    result_label: 'Результат:',
    result_correct_header: 'Правильна: ',
    question_header: 'Питання',
    your_answer_header: 'Ваша відповідь',
    correct_header_alt: 'Правильно',
    points_header: 'Бали',
    answers_available_later: 'ℹ️ Правильні відповіді будуть доступні після завершення тесту всіма учнями.',
    reload_later: 'Перезавантажте сторінку пізніше, щоб побачити правильні відповіді.',
    reload_page: '🔄 Перезавантажити сторінку',
  },
  en: {
    register_title: 'Register to start the quiz',
    register_button: 'Register',
    username_label: 'Username',
    waiting_title: 'Waiting for approval',
    waiting_description: 'Your registration is pending administrator approval.',
    waiting_hint: 'You can edit your data below and click "Check" to see if you have been approved.',
    save_changes: '💾 Save changes',
    check_status: '🔄 Check',
    not_approved_yet: 'You have not been approved yet. Please wait.',
    question_n_of_m: 'Question {n} of {m}',
    submit_answer: 'Submit answer',
    continue_button: 'Continue',
    explanation_title: '📖 Explanation',
    next_button: 'Next',
    enter_answer_label: '✍️ Enter your answer:',
    enter_answer_placeholder: 'Enter answer...',
    multiple_choice_hint: '📋 You can select multiple answers',
    points_label: 'Points:',
    points_2_4: 'points',
    points_other: 'points',
    time_left: 'Time left',
    time_is_up: 'Time is up',
    correct_answer_label: '✓ Correct answer',
    your_answer_label: '✗ Your answer',
    also_correct_label: '✓ Also correct',
    correct_answer_value: '✓ Correct answer: ',
    result_label: 'Result:',
    result_correct_header: 'Correct: ',
    question_header: 'Question',
    your_answer_header: 'Your answer',
    correct_header_alt: 'Correct',
    points_header: 'Points',
    answers_available_later: 'ℹ️ Correct answers will be available after all students complete the quiz.',
    reload_later: 'Reload the page later to see the correct answers.',
    reload_page: '🔄 Reload page',
  },
};

/** Student texts that are new in 2.0: 1.19 has nothing like them. */
export interface NewStudentTexts {
  answer_verdict_correct: string;
  answer_verdict_incorrect: string;
}

// internal/i18n/texts.go of the Go version ("new in 2.0")
export const NEW_TEXTS: Record<Language, NewStudentTexts> = {
  uk: {
    answer_verdict_correct: '✓ Правильно',
    answer_verdict_incorrect: '✗ Неправильно',
  },
  en: {
    answer_verdict_correct: '✓ Correct',
    answer_verdict_incorrect: '✗ Incorrect',
  },
};

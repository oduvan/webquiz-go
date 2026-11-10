// The only tool an AI agent teacher uses: one command per call.
//
//     npx tsx harness/teacher.ts NAME open admin
//     npx tsx harness/teacher.ts NAME click 5
//     npx tsx harness/teacher.ts NAME fill 7 "Біологія"
//
// Each call prints the step number, the time, what happened, the text on the
// screen and a numbered list of everything on the screen that can be used.
// The driver records the same step on its side.

const API = process.env.HARNESS_API ?? 'http://127.0.0.1:8198';

const USAGE = `The teacher's tool: one command per call.

    npx tsx harness/teacher.ts NAME open admin        (or: open files, open live-stats, open quiz)
    npx tsx harness/teacher.ts NAME look              show the screen again
    npx tsx harness/teacher.ts NAME click 5           click element [5]
    npx tsx harness/teacher.ts NAME fill 7 "text"     put this text into field [7] (replaces what is there)
    npx tsx harness/teacher.ts NAME replace 7 "old" "new"   change one part of the text in field [7]
    npx tsx harness/teacher.ts NAME select 12 "English"     choose an option in list [12]
    npx tsx harness/teacher.ts NAME check 9           turn checkbox [9] on (uncheck 9: off)
    npx tsx harness/teacher.ts NAME press Enter       press a key (Enter, Escape, Tab, ...)
    npx tsx harness/teacher.ts NAME wait 10           wait (up to 90 seconds)
    npx tsx harness/teacher.ts NAME reload            reload the page
    npx tsx harness/teacher.ts NAME close             close this tab and go back to the one before
    npx tsx harness/teacher.ts NAME students 5        5 automatic students join the quiz that runs now
`;

async function main(argv: string[]): Promise<number> {
  if (argv.length < 2) {
    console.log(USAGE);
    return 2;
  }
  const [name, cmd, ...args] = argv;
  try {
    const response = await fetch(API + '/teacher', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ teacher: name, cmd, args }),
      signal: AbortSignal.timeout(150_000),
    });
    console.log(await response.text());
    return response.ok ? 0 : 1;
  } catch (error) {
    const cause = error instanceof Error && error.cause instanceof Error ? error.cause.message : String(error);
    console.log(`The classroom is not running (${cause}).`);
    return 1;
  }
}

process.exit(await main(process.argv.slice(2)));
export {};

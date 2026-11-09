import { mountPage } from '../shared/mount';
import { Button } from '../shared/ui';

function CsvTablePage() {
  return (
    <main className="mx-auto w-full max-w-2xl p-4">
      <h1 className="text-xl font-bold">CSV table</h1>
      <p className="my-2">Placeholder page. The real page comes in a later task.</p>
      <Button>OK</Button>
    </main>
  );
}

mountPage(<CsvTablePage />);

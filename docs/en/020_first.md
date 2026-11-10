## First Run

This section will help you quickly launch **WebQuiz** in a local network.
The system is designed to work **inside your own network**, so you should not open its port to the public internet. This ensures that quiz results remain private and accessible only to users connected to your local environment.

---

### Download and Launch

The easiest way to start using **WebQuiz** is by running the prebuilt program. It is one file. You do not need to install Python or anything else.

1. Go to the [Releases](https://github.com/oduvan/webquiz-go/releases) page on the project’s GitHub repository.
2. Download the file that matches your computer:
   - **Windows:** `webquiz-windows-setup.exe` (installer) or `webquiz-windows.exe.zip` (portable). Windows 10 or newer.
   - **macOS:** `webquiz-macos-apple-silicon.zip` for Macs with an Apple chip (M1 and newer), or `webquiz-macos-intel.zip` for older Intel Macs. macOS 12 or newer.
   - **Linux:** `webquiz-linux.zip` for a normal 64-bit computer, `webquiz-linux-arm64.zip` for a 64-bit Raspberry Pi (3, 4, 5), or `webquiz-linux-arm.zip` for older and smaller Raspberry Pi boards (Zero, 1, 2).
3. If the file comes as an archive, extract it into any folder you prefer.
4. Run the program:
   - **Windows (Installer):** run `webquiz-windows-setup.exe` and start WebQuiz from the Start Menu.
   - **Windows (Portable):** extract `webquiz-windows.exe.zip`, then double-click the `.exe` file or run it from the command line.
   - **macOS / Linux:** first allow the file to run. Use the real file name from the archive, for example:
     ```
     chmod +x webquiz-linux
     ```
     then start it:
     ```
     ./webquiz-linux
     ```

WebQuiz keeps all its files in the folder where the program lies (see [Files and Folders Created](#files-and-folders-created)). Put the program in its own folder, for example `WebQuiz`.

#### Important for macOS Users

On first launch on macOS, you'll see a security warning because the binary is not signed by Apple:

![First launch warning](../imgs/mac_first_launch.png)

To allow the application to run:

1. Click **Done** in the warning dialog
2. Open **System Settings** → **Privacy & Security**
3. Scroll to the **Security** section and click **Open Anyway**:

![Open Anyway](../imgs/mac_open_anyway.png)

4. Confirm by clicking **Open Anyway** in the dialog:

![Confirmation dialog](../imgs/mac_open_anyway_2.png)

WebQuiz will now launch normally. This is a one-time procedure — subsequent launches won't require confirmation.

#### Important for Windows Users

Windows Defender or other antivirus software may sometimes warn about the program, although it is safe. New programs that few people have downloaded yet are often flagged by mistake. This is called a false positive.

WebQuiz is open-source software. You can:
- Review the source code on [GitHub](https://github.com/oduvan/webquiz-go)
- Add the file to Windows Defender exclusions

If you see a warning, allow the program to run or add it to your antivirus's trusted applications list.

---

5. After launching, the admin panel page will open in your browser at [http://localhost:8080/admin/](http://localhost:8080/admin/):

![First admin panel launch](../imgs/first_admin.png)

The admin panel shows:
- The list of available quizzes with the current quiz highlighted
- Buttons to switch, edit, duplicate, or delete quizzes
- The local network URL for other devices to connect
- Quick links to **File Manager** and **Live Stats** at the bottom
- The current WebQuiz version and a link to the documentation
- A theme switcher (sun/moon icon) in the top-right corner for dark/light mode

---

### Files and Folders Created

After the first launch, WebQuiz will automatically create several service files and folders in the same directory as the executable.
They are used to store configuration, results, and diagnostic information.

| File / Folder | Purpose |
|---------------|----------|
| **webquiz.yaml** | The main configuration file. Stores system parameters, including user registration settings and general options. |
| **quizzes/** | Directory for storing quizzes in YAML format. You can create your own question files or edit the default example that comes included. Pictures go to `quizzes/imgs/` and files for download go to `quizzes/attach/`. |
| **data/** | Quiz results in CSV format will be stored here. They can be viewed or opened in spreadsheet editors. |
| **logs/** | Directory containing application logs. Records messages about launches, errors, or warnings. If something goes wrong during testing, these files will help identify the cause. |
| **static/** | Contains the quiz page that students see. Typically, you don't need to edit these files. |

To keep these files in another folder, set the environment variable `WEBQUIZ_BINARY_DIR` to that folder before you start the program.

---

### The First Quiz

Right after the first run, WebQuiz automatically creates a simple example quiz.
This allows you to immediately test the system without any additional configuration.

To take the quiz:

1. Open [http://localhost:8080](http://localhost:8080) in your browser.

![First quiz launch](../imgs/reg_default_test.png)

2. Enter your name (or any test name if you're just checking functionality).
3. Click the **Register** button — the system will load the example quiz from the configuration file, and you can complete it like a normal user.
4. After finishing the quiz, open the **data** folder — you'll find a file with the results in CSV format. If needed, also check the **logs** folder to ensure the server is running without errors.

![Quiz completion](../imgs/reg_default_test.png)

#### If the Connection Drops

Wi-Fi in a classroom is not always stable. When the student's device loses the connection to the server, the quiz page does not show an error. It shows a yellow banner at the top, **"Connection to the server lost. Retrying..."**, and keeps repeating the request every few seconds until the server answers. The chosen answer is locked while the page waits, and the quiz continues on its own once the connection is back.

An answer or a registration is never counted twice, even if the server got the first attempt and only the reply was lost. A page opened or reloaded while the server is unreachable also waits for the connection instead of showing the registration form again.

---

### Build from Source and Command-Line Options

If you want to build WebQuiz yourself, you need Go and Node.js:

```
git clone https://github.com/oduvan/webquiz-go.git
cd webquiz-go
make build
./bin/webquiz
```

Additional command-line options:
- `--config` — specify a custom configuration file path.
- `--port` — the port to listen on (default: 8080).
- `--master-key` — the master key for the admin panel.
- `--quizzes-dir` — the folder with quiz files.
- `--csv-dir` — define a custom location for saving results.
- `--logs-dir` — change the folder of the log files.
- `-d` or `--daemon` — run the server in background mode (not on Windows).
- `--stop`, `--status` — stop or check the status of the server.
- `--version` — print the version and exit.

These capabilities will come in handy if you want to use WebQuiz as part of a larger system or automatically start it when your device boots.

# Cubicle desktop

The Windows app of [Cubicle](../README.md), built with C# / .NET 10 WPF and the Fluent theme.

```
Cubicle.slnx
├── src/Cubicle.Client/          UI-free gateway client (REST + SSE), models, schedules
├── src/Cubicle.Desktop/         WPF app → Cubicle.exe
│   ├── Views/  ViewModels/  Services/  Converters/  Themes/  Assets/
└── tests/Cubicle.Client.Tests/  xUnit tests
```

```powershell
dotnet build Cubicle.slnx
dotnet test Cubicle.slnx
dotnet run --project src/Cubicle.Desktop     # expects a gateway on 127.0.0.1:7878 (see scripts/dev.ps1)
```

Command-line options: `--background` starts the app in the tray (used for "start with Windows").
Environment overrides: `CUBICLE_GATEWAY_URL`, `CUBICLE_HOME`, and `CUBICLE_GATEWAY_EXE` (the gateway binary to launch).

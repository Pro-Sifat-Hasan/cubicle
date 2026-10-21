namespace Cubicle.Client.Tests;

public class CubiclePathsTests
{
    [Fact]
    public void Reads_token_and_rejects_short_ones()
    {
        var dir = Directory.CreateTempSubdirectory();
        try
        {
            var file = Path.Join(dir.FullName, "admin.token");
            Assert.Null(CubiclePaths.TryReadAdminToken(file));

            File.WriteAllText(file, "short");
            Assert.Null(CubiclePaths.TryReadAdminToken(file));

            var token = new string('a', 64);
            File.WriteAllText(file, token + "\n");
            Assert.Equal(token, CubiclePaths.TryReadAdminToken(file));
        }
        finally
        {
            dir.Delete(recursive: true);
        }
    }
}

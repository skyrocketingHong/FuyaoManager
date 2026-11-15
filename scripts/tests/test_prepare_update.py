"""Exercise the actual fixed helper's URI conversion without running a deploy."""
import ast
from pathlib import Path
import unittest

helper = Path(__file__).resolve().parents[2] / 'deploy/fuyao-admin/prepare-update.sh'
source = helper.read_text().split("<<'PY'\n", 1)[1].split('\nPY\n', 1)[0]
tree = ast.parse(source)
definitions = [node for node in tree.body if isinstance(node, (ast.Import, ast.ImportFrom, ast.FunctionDef))]
namespace = {}
exec(compile(ast.Module(body=definitions, type_ignores=[]), str(helper), 'exec'), namespace)
connection_env = namespace['connection_env']


class ConnectionEnvironmentTests(unittest.TestCase):
    def test_uri_fields_are_discrete_and_percent_decoded(self):
        result = connection_env('postgresql://service:p%40ss%3Aword@127.0.0.1:5544/fuyao_mvp_test?sslmode=disable', {'PATH': '/usr/bin', 'PGUSER': 'root', 'PGSERVICE': 'unrelated'})
        self.assertEqual(result['PGUSER'], 'service')
        self.assertEqual(result['PGPASSWORD'], 'p@ss:word')
        self.assertEqual(result['PGHOST'], '127.0.0.1')
        self.assertEqual(result['PGPORT'], '5544')
        self.assertEqual(result['PGDATABASE'], 'fuyao_mvp_test')
        self.assertEqual(result['PGSSLMODE'], 'disable')
        self.assertNotIn('PGSERVICE', result)

    def test_ipv6_and_tls_options(self):
        result = connection_env('postgres://service:password@[::1]/fuyao_mvp_test?sslmode=verify-full&connect_timeout=5&application_name=updater', {})
        self.assertEqual(result['PGHOST'], '::1')
        self.assertEqual(result['PGPORT'], '5432')
        self.assertEqual(result['PGCONNECT_TIMEOUT'], '5')
        self.assertEqual(result['PGAPPNAME'], 'updater')

    def test_missing_identity_and_unknown_options_are_rejected(self):
        for uri in ('postgres://localhost/database', 'file:///etc/passwd', 'postgres://user@localhost/db?service=unrelated'):
            with self.assertRaises(ValueError):
                connection_env(uri, {})


if __name__ == '__main__':
    unittest.main()

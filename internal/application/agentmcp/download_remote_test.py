import importlib.util
import contextlib
import gzip
import io
import os
import sys
import tempfile
import unittest
from unittest import mock

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location('download', os.path.join(os.path.dirname(__file__), 'download_remote.py'))
download = importlib.util.module_from_spec(spec)
spec.loader.exec_module(download)


class CaptureTests(unittest.TestCase):
    @unittest.skipUnless(os.name == 'posix', 'requires POSIX directory descriptors')
    def test_prepare_without_content_hash(self):
        for filename in ('app.log', 'original.gz'):
            with self.subTest(filename=filename), tempfile.TemporaryDirectory() as directory:
                stage = os.path.join(directory, 'stage')
                os.mkdir(stage)
                source = os.path.join(directory, filename)
                content = b'bounded log\n' * 4096
                with open(source, 'wb') as file:
                    file.write(content)
                with mock.patch('hashlib.sha256', side_effect=AssertionError('不应计算文件内容哈希')), contextlib.redirect_stderr(io.StringIO()):
                    result = download.prepare(dict(paths=[source], stage=stage, stem='.jumpaccess-download-test', marker='test'))
                self.assertNotIn('sha256', result)
                self.assertNotIn('sha256', result['entries'][0])
                self.assertEqual(result['size'], os.stat(result['path']).st_size)
                self.assertEqual(result['entries'][0]['size'], len(content))
                with open(result['path'], 'rb') as file:
                    actual = file.read()
                if result['format'] == 'gzip':
                    actual = gzip.decompress(actual)
                self.assertEqual(actual, content)

    def test_appends_do_not_extend_capture(self):
        with tempfile.TemporaryDirectory() as directory:
            name = os.path.join(directory, 'app.log')
            with open(name, 'wb') as f:
                f.write(b'original')
            with download.Capture(os.open(name, os.O_RDONLY)) as capture:
                with open(name, 'ab') as f:
                    f.write(b'new content')
                self.assertEqual(capture.read(1000), b'original')
                self.assertEqual(capture.read(1000), b'')
                self.assertEqual(capture.size, 8)

    def test_short_read_is_source_changed(self):
        with tempfile.TemporaryDirectory() as directory:
            name = os.path.join(directory, 'app.log')
            with open(name, 'wb') as f:
                f.write(b'original')
            with download.Capture(os.open(name, os.O_RDONLY)) as capture:
                with open(name, 'wb') as f:
                    f.write(b'x')
                with self.assertRaisesRegex(download.Problem, 'SOURCE_CHANGED'):
                    capture.read(1000)

    @unittest.skipUnless(os.name == 'posix', 'requires POSIX inode semantics')
    def test_rename_rotation_preserves_open_file(self):
        with tempfile.TemporaryDirectory() as directory:
            name = os.path.join(directory, 'app.log')
            with open(name, 'wb') as f:
                f.write(b'original')
            with download.Capture(os.open(name, os.O_RDONLY)) as capture:
                os.rename(name, name + '.1')
                with open(name, 'wb') as f:
                    f.write(b'new log')
                self.assertEqual(capture.read(1000), b'original')

    @unittest.skipUnless(os.name == 'posix', 'requires POSIX directory descriptors')
    def test_inventory_rejects_symlinks_and_own_staging(self):
        with tempfile.TemporaryDirectory() as directory:
            name = os.path.join(directory, 'app.log')
            with open(name, 'wb') as f:
                f.write(b'original')
            os.symlink(name, name + '.link')
            with self.assertRaises(OSError):
                download.inventory([name + '.link'], '/tmp')
            with self.assertRaisesRegex(download.Problem, 'STAGING_INSIDE_SOURCE'):
                download.inventory([directory], directory)


if __name__ == '__main__':
    unittest.main()

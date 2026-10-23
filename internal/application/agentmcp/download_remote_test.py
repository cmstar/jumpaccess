import importlib.util
import contextlib
import gzip
import io
import os
import sys
import tempfile
import tarfile
import unittest
from unittest import mock

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location('download', os.path.join(os.path.dirname(__file__), 'download_remote.py'))
download = importlib.util.module_from_spec(spec)
spec.loader.exec_module(download)


@contextlib.contextmanager
def physical_temporary_directory():
    # macOS 的临时路径可能经过 /var 链接，样例本身仍须满足拒绝父目录链接的规则。
    with tempfile.TemporaryDirectory() as directory:
        yield os.path.realpath(directory)


class CaptureTests(unittest.TestCase):
    def test_transmission_limit_counts_only_written_bytes(self):
        target = io.BytesIO()
        writer = download.LimitedWriter(target, 4)
        self.assertEqual(writer.write(b'abcd'), 4)
        with self.assertRaisesRegex(download.Problem, 'TRANSFER_TOO_LARGE'):
            writer.write(b'e')
        self.assertEqual(target.getvalue(), b'abcd')

    @unittest.skipUnless(os.name == 'posix', 'requires POSIX directory descriptors')
    def test_prepare_deduplicates_containment_and_preserves_empty_directory(self):
        with physical_temporary_directory() as directory:
            stage = os.path.join(directory, 'stage')
            source = os.path.join(directory, 'logs')
            os.mkdir(stage)
            os.mkdir(source)
            child = os.path.join(source, 'empty')
            os.mkdir(child)
            with contextlib.redirect_stderr(io.StringIO()):
                result = download.prepare(dict(paths=[child, source, source], stage=stage, stem='.jumpaccess-download-test', marker='test', result_name='JumpAccess-test'))
            self.assertEqual(result['format'], 'tar.gz')
            self.assertFalse(result['wrapped'])
            self.assertEqual(result['root_name'], 'logs')
            with tarfile.open(result['path']) as archive:
                self.assertEqual(archive.getnames(), ['logs', 'logs/empty'])

    @unittest.skipUnless(os.name == 'posix', 'requires POSIX directory descriptors')
    def test_multi_source_archive_wraps_once_and_keeps_duplicate_basenames(self):
        with physical_temporary_directory() as directory:
            stage = os.path.join(directory, 'stage')
            os.mkdir(stage)
            sources = []
            for name in ('a', 'b'):
                parent = os.path.join(directory, name)
                os.mkdir(parent)
                source = os.path.join(parent, 'app.log')
                with open(source, 'wb') as file:
                    file.write(name.encode())
                sources.append(source)
            with contextlib.redirect_stderr(io.StringIO()):
                result = download.prepare(dict(paths=sources, stage=stage, stem='.jumpaccess-download-test', marker='test', result_name='JumpAccess-test'))
            self.assertTrue(result['wrapped'])
            self.assertEqual(result['suggested_name'], 'JumpAccess-test.tar.gz')
            with tarfile.open(result['path']) as archive:
                self.assertEqual(archive.getnames(), ['JumpAccess-test', 'JumpAccess-test/app.log', 'JumpAccess-test/app.log (2)'])

    @unittest.skipUnless(os.name == 'posix', 'requires POSIX directory descriptors')
    def test_prepare_limits_compressed_artifact_and_leaves_only_owned_partial(self):
        with physical_temporary_directory() as directory:
            stage = os.path.join(directory, 'stage')
            os.mkdir(stage)
            source = os.path.join(directory, 'app.log')
            with open(source, 'wb') as file:
                file.write(os.urandom(4096))
            with self.assertRaisesRegex(download.Problem, 'TRANSFER_TOO_LARGE'), contextlib.redirect_stderr(io.StringIO()):
                download.prepare(dict(paths=[source], stage=stage, stem='.jumpaccess-download-test', marker='test', max_bytes=100))
            self.assertEqual(os.listdir(stage), ['.jumpaccess-download-test.gz.part'])
            self.assertLessEqual(os.stat(os.path.join(stage, os.listdir(stage)[0])).st_size, 100)

    @unittest.skipUnless(os.name == 'posix', 'requires POSIX directory descriptors')
    def test_compressed_source_size_does_not_use_transmission_limit(self):
        with physical_temporary_directory() as directory:
            stage = os.path.join(directory, 'stage')
            os.mkdir(stage)
            source = os.path.join(directory, 'app.log')
            with open(source, 'wb') as file:
                file.write(b'a' * 100000)
            with contextlib.redirect_stderr(io.StringIO()):
                result = download.prepare(dict(paths=[source, source], stage=stage, stem='.jumpaccess-download-test', marker='test', max_bytes=1000))
            self.assertEqual(result['format'], 'gzip')
            self.assertEqual(result['suggested_name'], 'app.log.gz')
            self.assertEqual(len(result['entries']), 1)
            self.assertLessEqual(result['size'], 1000)
            self.assertEqual(result['entries'][0]['size'], 100000)

    @unittest.skipUnless(os.name == 'posix', 'requires POSIX directory descriptors')
    def test_containment_does_not_hide_invalid_child(self):
        with physical_temporary_directory() as directory:
            with self.assertRaises(FileNotFoundError):
                download.independent_paths([directory, os.path.join(directory, 'missing')])
            child = os.path.join(directory, 'link')
            os.symlink('/tmp', child)
            with self.assertRaises(OSError):
                download.independent_paths([directory, child])

    @unittest.skipUnless(os.name == 'posix', 'requires POSIX directory descriptors')
    def test_prepare_without_content_hash(self):
        for filename in ('app.log', 'original.gz'):
            with self.subTest(filename=filename), physical_temporary_directory() as directory:
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
        with physical_temporary_directory() as directory:
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
        with physical_temporary_directory() as directory:
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
        with physical_temporary_directory() as directory:
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
        with physical_temporary_directory() as directory:
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

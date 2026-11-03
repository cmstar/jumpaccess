import contextlib
import gzip
import io
import os
import tarfile
import tempfile
import unittest
from unittest import mock

import upload_remote as upload


class UploadRemoteTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = os.path.realpath(self.temp.name)
        self.destination = os.path.join(self.base, 'dest')
        os.mkdir(self.destination)
        with contextlib.redirect_stderr(io.StringIO()):
            self.stage = upload.create_stage(dict(stage=self.base, name='.jumpaccess-upload-test', marker='test'))

    def package(self, data, entries, mode='gzip'):
        filename = os.path.join(self.stage['path'], 'payload')
        with open(filename, 'wb') as f:
            f.write(data)
        return dict(stage=self.stage, destination=self.destination, format=mode, size=len(data), entries=entries, marker='test')

    def test_gzip_publish_and_cleanup_leave_source_and_final(self):
        data = b'configuration\n' * 100
        request = self.package(gzip.compress(data), [dict(name='config.txt', size=len(data), directory=False)])
        with contextlib.redirect_stderr(io.StringIO()):
            result = upload.restore(request)
        self.assertEqual(result['files'][0]['status'], 'completed')
        with open(os.path.join(self.destination, 'config.txt'), 'rb') as f:
            self.assertEqual(f.read(), data)
        upload.cleanup(dict(files=[self.stage, result['restore_stage']]))
        self.assertFalse(os.path.exists(self.stage['path']))
        self.assertEqual(os.listdir(self.destination), ['config.txt'])

    def test_conflict_never_overwrites(self):
        filename = os.path.join(self.destination, 'config.txt')
        with open(filename, 'wb') as f: f.write(b'old')
        request = self.package(gzip.compress(b'new'), [dict(name='config.txt', size=3, directory=False)])
        with self.assertRaisesRegex(upload.Problem, 'TARGET_CONFLICT'):
            upload.restore(request)
        with open(filename, 'rb') as f: self.assertEqual(f.read(), b'old')

    def test_tar_empty_directory_and_original_archive(self):
        stream = io.BytesIO()
        entries = [dict(name='config', directory=True, size=0), dict(name='config/empty', directory=True, size=0), dict(name='config/archive.zip', directory=False, size=4)]
        with tarfile.open(fileobj=stream, mode='w:gz') as archive:
            for entry in entries:
                h = tarfile.TarInfo(entry['name']); h.size = entry['size']
                h.type = tarfile.DIRTYPE if entry['directory'] else tarfile.REGTYPE
                archive.addfile(h, None if entry['directory'] else io.BytesIO(b'PKxx'))
        with contextlib.redirect_stderr(io.StringIO()): result = upload.restore(self.package(stream.getvalue(), entries, 'tar.gz'))
        self.assertTrue(os.path.isdir(os.path.join(self.destination, 'config/empty')))
        self.assertEqual(result['files'][0]['path'], os.path.join(self.destination, 'config'))

    def test_unsafe_or_incomplete_archive_does_not_publish(self):
        for name, kind in [('../escape', tarfile.REGTYPE), ('safe', tarfile.SYMTYPE), ('/absolute', tarfile.REGTYPE)]:
            with self.subTest(name=name):
                stream = io.BytesIO()
                with tarfile.open(fileobj=stream, mode='w:gz') as archive:
                    h = tarfile.TarInfo(name); h.type = kind; h.linkname = '../escape'; archive.addfile(h)
                request = self.package(stream.getvalue(), [dict(name='safe', size=0, directory=False)], 'tar.gz')
                with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(upload.Problem): upload.restore(request)
                self.assertFalse(os.path.exists(os.path.join(self.destination, 'safe')))
                self.assertFalse(os.path.exists(os.path.join(self.base, 'escape')))
        request = self.package(gzip.compress(b'x')[:-3], [dict(name='file', size=1, directory=False)])
        with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(upload.Problem): upload.restore(request)
        self.assertFalse(os.path.exists(os.path.join(self.destination, 'file')))

    def test_racing_target_and_partial_publication(self):
        entries = [dict(name='a', size=0, directory=True), dict(name='b', size=0, directory=True)]
        stream = io.BytesIO()
        with tarfile.open(fileobj=stream, mode='w:gz') as archive:
            for e in entries:
                h=tarfile.TarInfo(e['name']); h.type=tarfile.DIRTYPE; archive.addfile(h)
        publish = upload.rename_noreplace
        def race(srcfd, src, dstfd, dst):
            if dst == 'b': os.mkdir(dst, dir_fd=dstfd)
            publish(srcfd, src, dstfd, dst)
        with mock.patch.object(upload, 'rename_noreplace', side_effect=race), contextlib.redirect_stderr(io.StringIO()):
            result = upload.restore(self.package(stream.getvalue(), entries, 'tar.gz'))
        self.assertEqual([f['status'] for f in result['files']], ['completed', 'failed'])
        self.assertEqual(result['error'], 'TARGET_CONFLICT')
        self.assertTrue(os.path.isdir(os.path.join(self.destination, 'a')))

    def test_cleanup_refuses_replaced_directory(self):
        os.rename(self.stage['path'], self.stage['path'] + '-old')
        os.mkdir(self.stage['path'])
        with self.assertRaisesRegex(upload.Problem, 'TEMPORARY_FILE_CHANGED'): upload.cleanup(dict(files=[self.stage]))
        self.assertTrue(os.path.isdir(self.stage['path']))


if __name__ == '__main__': unittest.main()

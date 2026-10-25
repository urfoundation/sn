// Package wincustody gives Windows the private-directory custody the Unix
// stores build from openat, O_NOFOLLOW, fstat and flock.
//
// A directory is opened one path component at a time from its drive's root,
// each component relative to the last and none a reparse point, and every
// leaf operation is relative to that handle (NtCreateFile and
// NtSetInformationFile with a root directory). Privacy is the security
// descriptor: an owner that is this user, SYSTEM or Administrators, and a DACL
// that grants nobody else what the Unix rule refuses. A private directory
// (mode&022 == 0) leaves others at most read and traverse rights; a private
// file (mode&077 == 0) leaves them nothing. New custody is created with a
// protected descriptor granting only this user, SYSTEM and Administrators,
// so it never inherits a wider one.
//
// This package does not flush directories (clientauth's registration store,
// which keeps a writable directory handle open, does). A rename is made
// durable the way MoveFileEx(MOVEFILE_WRITE_THROUGH) makes it: by flushing
// the renamed file's handle, which commits the NTFS log record of the rename. A durable removal
// first renames the leaf aside that way, so its name is durably gone before
// the renamed leftover is deleted.
//
// It backs the miner's wallet consent custody and the friendly CLI's
// state folder check. Only the Windows build has an implementation; the
// package is empty elsewhere.
package wincustody

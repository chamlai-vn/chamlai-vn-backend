package mimetypes

// ContentType is a MIME type string for files supported by the RAG pipeline.
type ContentType string

const (
	ContentTypeImagePNG  ContentType = "image/png"
	ContentTypeImageJPG  ContentType = "image/jpg"
	ContentTypeImageJPEG ContentType = "image/jpeg"

	ContentTypePlain ContentType = "text/plain"
	ContentTypePDF   ContentType = "application/pdf"
	ContentTypeCSV   ContentType = "text/csv"

	ContentTypeDOCX ContentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	ContentTypeXLSX ContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	ContentTypePPTX ContentType = "application/vnd.openxmlformats-officedocument.presentationml.presentation"

	ContentTypeVideoMOV        ContentType = "video/mov"
	ContentTypeVideoMP4        ContentType = "video/mp4"
	ContentTypeVideoQuickTime  ContentType = "video/quicktime"
	ContentTypeVideoXQuickTime ContentType = "video/x-quicktime"

	ContentTypeAudioMPEG   ContentType = "audio/mpeg"
	ContentTypeAudioMPEG3  ContentType = "audio/mpeg3"
	ContentTypeAudioXMPEG3 ContentType = "audio/x-mpeg-3"
	ContentTypeAudioWav    ContentType = "audio/wav"
	ContentTypeAudioXWav   ContentType = "audio/x-wav"
)

package main

import (
	"bytes"
	"mime/multipart"
	"encoding/json"
	"io"
	"log"
	"os"
	"net/http"
	"net/http/httptest"
	"testing"
	"fmt"
	"database/sql"
	
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
)

// createTestApp creates a test instance of the application with mocked dependencies.
func createTestApp(t *testing.T) (*App, sqlmock.Sqlmock) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Error creating sqlmock: %v", err)
	}

	logger := log.New(io.Discard, "", log.LstdFlags) 

	return &App{
		DB:     db,
		Logger: logger,
	}, mock
}

// Test for getEnv function using Dependency Injection
func TestGetEnv(t *testing.T) {
	originalValue, isSet := os.LookupEnv("TEST_KEY")
	defer func() {
		if isSet {
			os.Setenv("TEST_KEY", originalValue)
		} else {
			os.Unsetenv("TEST_KEY")
		}
	}()

	os.Setenv("TEST_KEY", "expected_value")
	actual := getEnv("TEST_KEY", "default_value")
	assert.Equal(t, "expected_value", actual)

	os.Unsetenv("TEST_KEY")
	actual = getEnv("TEST_KEY", "default_value")
	assert.Equal(t, "default_value", actual)
}

func TestInitDB(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	assert.NoError(t, err, "Error should be nil when creating sqlmock")
	defer db.Close()

	dsn := "user:password@tcp(localhost:3306)/testdb"


	originalSQLOpen := sqlOpen  
	sqlOpen = func(driverName, dataSourceName string) (*sql.DB, error) {
		if dataSourceName == dsn {
			return db, nil
		}
		return nil, fmt.Errorf("unexpected DSN: %s", dataSourceName)
	}

	defer func() { sqlOpen = originalSQLOpen }()

	t.Run("Successful Database Initialization", func(t *testing.T) {
		mock.ExpectPing()

		_, err = initDB("user", "password", "localhost", "3306", "testdb")
		assert.NoError(t, err, "Error should be nil when initializing the DB")

		err = mock.ExpectationsWereMet()
		assert.NoError(t, err, "There should be no unmet expectations")
	})

	t.Run("Failed to Open Database Connection", func(t *testing.T) {
		sqlOpen = func(driverName, dataSourceName string) (*sql.DB, error) {
			return nil, fmt.Errorf("failed to connect to the database")
		}

		_, err = initDB("invalid_user", "invalid_password", "localhost", "3306", "testdb")
		assert.Error(t, err, "Expected an error when failing to connect to the database")
		assert.Contains(t, err.Error(), "failed to connect to the database")

		sqlOpen = originalSQLOpen
	})

	t.Run("Failed to Ping Database", func(t *testing.T) {
		mock.ExpectPing().WillReturnError(fmt.Errorf("failed to ping"))

		sqlOpen = func(driverName, dataSourceName string) (*sql.DB, error) {
			if dataSourceName == dsn {
				return db, nil
			}
			return nil, fmt.Errorf("unexpected DSN: %s", dataSourceName)
		}

		_, err = initDB("user", "password", "localhost", "3306", "testdb")
		assert.Error(t, err, "Expected an error when pinging the database")
		assert.Contains(t, err.Error(), "failed to ping the database")

		err = mock.ExpectationsWereMet()
		assert.NoError(t, err, "There should be no unmet expectations")
	})
}

// TestHome tests the Home handler
func TestHome(t *testing.T) {
	app, _ := createTestApp(t) 
	defer app.DB.Close()

	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatalf("Could not create request: %v", err)
	}

	rr := httptest.NewRecorder()

	handler := http.HandlerFunc(app.Home)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "Expected status code 200")

	expectedBody := "Homepage"
	assert.Equal(t, expectedBody, rr.Body.String(), "Expected response body 'Homepage'")
}

// TestInfo tests the Info handler
func TestInfo(t *testing.T) {
	app, _ := createTestApp(t) 
	defer app.DB.Close()

	req, err := http.NewRequest("GET", "/info", nil)
	if err != nil {
		t.Fatalf("Could not create request: %v", err)
	}

	rr := httptest.NewRecorder()

	handler := http.HandlerFunc(app.Info)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "Expected status code 200")

	expectedBody := "Info page"
	assert.Equal(t, expectedBody, rr.Body.String(), "Expected response body 'Info page'")
}

// TestSetupRouter verifies that all routes are correctly set up in the router
func TestSetupRouter(t *testing.T) {
	app, mock := createTestApp(t)
	defer app.DB.Close()

	router := app.setupRouter()

	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
		mockSetup      func()
	}{
		{
			name:           "Get all subscribers",
			method:         "GET",
			path:           "/subscribers",
			expectedStatus: http.StatusOK,
			mockSetup: func() {
				rows := sqlmock.NewRows([]string{"lastname", "firstname", "email"}).
					AddRow("Doe", "John", "john.doe@example.com").
					AddRow("Smith", "Jane", "jane.smith@example.com")
				mock.ExpectQuery(`SELECT lastname, firstname, email FROM subscribers`).WillReturnRows(rows)
			},
		},
		{
			name:           "Get all books",
			method:         "GET",
			path:           "/books",
			expectedStatus: http.StatusOK,
			mockSetup: func() {
				rows := sqlmock.NewRows([]string{
					"book_id", "book_title", "author_id", "book_photo", "is_borrowed", "book_details", "author_lastname", "author_firstname",
				}).
					AddRow(1, "Sample Book", 1, "book.jpg", false, "A sample book", "Doe", "John").
					AddRow(2, "Another Book", 2, "another.jpg", true, "Another sample book", "Smith", "Jane")

				mock.ExpectQuery(`SELECT books.id AS book_id, books.title AS book_title, books.author_id AS author_id, books.photo AS book_photo, books.is_borrowed AS is_borrowed, books.details AS book_details, authors.Lastname AS author_lastname, authors.Firstname AS author_firstname FROM books JOIN authors ON books.author_id = authors.id`).WillReturnRows(rows)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			
			tt.mockSetup()

			req, err := http.NewRequest(tt.method, tt.path, nil)
			if err != nil {
				t.Fatalf("Could not create request: %v", err)
			}
			rr := httptest.NewRecorder()

			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.expectedStatus, rr.Code)

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Not all expectations were met: %v", err)
			}
		})
	}
}

// TestRespondWithJSON tests the RespondWithJSON function
func TestRespondWithJSON(t *testing.T) {
    rr := httptest.NewRecorder()

    payload := map[string]string{"message": "success"}

    RespondWithJSON(rr, http.StatusOK, payload)
  
    assert.Equal(t, "application/json", rr.Header().Get("Content-Type"), "Content-Type should be application/json")

    assert.Equal(t, http.StatusOK, rr.Code, "Expected status code 200")

    expectedBody, _ := json.Marshal(payload)
    assert.JSONEq(t, string(expectedBody), rr.Body.String(), "Response body should match the payload")
}

func TestRespondWithJSON_Success(t *testing.T) {
    rr := httptest.NewRecorder()
    payload := map[string]string{"message": "test"}

    RespondWithJSON(rr, http.StatusOK, payload)

    assert.Equal(t, "application/json", rr.Header().Get("Content-Type"), "Expected Content-Type application/json")
    assert.Equal(t, http.StatusOK, rr.Code, "Expected status code 200")
    assert.JSONEq(t, `{"message": "test"}`, rr.Body.String(), "Expected JSON response")
}

func TestRespondWithJSON_Error(t *testing.T) {
    rr := httptest.NewRecorder()
    payload := make(chan int)

    RespondWithJSON(rr, http.StatusOK, payload)

    assert.Equal(t, "application/json", rr.Header().Get("Content-Type"), "Expected Content-Type application/json")
    assert.Equal(t, http.StatusInternalServerError, rr.Code, "Expected status code 500 for encoding error")
    assert.Equal(t, "Error encoding response\n", rr.Body.String(), "Expected error message in response")
}

// TestHandleError tests the HandleError function
func TestHandleError(t *testing.T) {
    rr := httptest.NewRecorder()
    logger := log.New(io.Discard, "", log.LstdFlags) // Logger care nu afiseaza nimic
    message := "test error"
    err := fmt.Errorf("an example error")

    HandleError(rr, logger, message, err, http.StatusInternalServerError)

    assert.Equal(t, http.StatusInternalServerError, rr.Code, "Expected status code 500")
    assert.Equal(t, "test error\n", rr.Body.String(), "Expected error message in response")
}

// TestGetIDFromRequest tests the GetIDFromRequest function
func TestGetIDFromRequest(t *testing.T) {
    req := httptest.NewRequest("GET", "/authors/1", nil)
    req = mux.SetURLVars(req, map[string]string{"id": "1"})

    id, err := GetIDFromRequest(req, "id")
    assert.NoError(t, err, "Expected no error for a valid ID")
    assert.Equal(t, 1, id, "Expected ID to be 1")

    req = httptest.NewRequest("GET", "/authors/abc", nil)
    req = mux.SetURLVars(req, map[string]string{"id": "abc"})

    _, err = GetIDFromRequest(req, "id")
    assert.Error(t, err, "Expected an error for an invalid ID")
    assert.Contains(t, err.Error(), "invalid id", "Error message should mention 'invalid id'")
}

func TestValidateBookData(t *testing.T) {
    book := Book{Title: "Valid Book Title", AuthorID: 1}
    err := ValidateBookData(book)
    assert.NoError(t, err, "Expected no error for valid book data")

    book = Book{Title: "", AuthorID: 1}
    err = ValidateBookData(book)
    assert.Error(t, err, "Expected an error for missing title")
    assert.Contains(t, err.Error(), "title and authorID are required fields", "Error message should mention missing title")

    book = Book{Title: "Valid Book Title", AuthorID: 0}
    err = ValidateBookData(book)
    assert.Error(t, err, "Expected an error for missing author ID")
    assert.Contains(t, err.Error(), "title and authorID are required fields", "Error message should mention missing author ID")

    book = Book{Title: "", AuthorID: 0}
    err = ValidateBookData(book)
    assert.Error(t, err, "Expected an error for missing title and author ID")
    assert.Contains(t, err.Error(), "title and authorID are required fields", "Error message should mention missing fields")
}

// TestScanAuthors tests the ScanAuthors function
func TestScanAuthors(t *testing.T) {
    db, mock, err := sqlmock.New()
    assert.NoError(t, err, "Error should be nil when creating sqlmock")
    defer db.Close()

    rows := sqlmock.NewRows([]string{"id", "lastname", "firstname", "photo"}).
        AddRow(1, "Doe", "John", "photo.jpg").
        AddRow(2, "Smith", "Jane", "photo2.jpg")

    mock.ExpectQuery(`SELECT id, lastname, firstname, photo FROM authors`).WillReturnRows(rows)

    result, err := db.Query("SELECT id, lastname, firstname, photo FROM authors")
    assert.NoError(t, err, "Query execution should not return an error")
    authors, err := ScanAuthors(result)
    assert.NoError(t, err, "Expected no error while scanning authors")
    assert.Equal(t, 2, len(authors), "Expected 2 authors")
    assert.Equal(t, "John", authors[0].Firstname, "Expected Firstname to be John")
    assert.Equal(t, "Doe", authors[0].Lastname, "Expected Lastname to be Doe")
}

func TestScanAuthors_ErrorAfterIteration(t *testing.T) {
    db, mock, err := sqlmock.New()
    assert.NoError(t, err, "Eroarea ar trebui să fie nil la crearea sqlmock")
    defer db.Close()

    rows := sqlmock.NewRows([]string{"id", "lastname", "firstname", "photo"}).
        AddRow(1, "Doe", "John", "photo.jpg").
        AddRow(2, "Smith", "Jane", "photo2.jpg").
        RowError(1, fmt.Errorf("iteration error")) 

    mock.ExpectQuery(`SELECT id, lastname, firstname, photo FROM authors`).WillReturnRows(rows)

    result, err := db.Query("SELECT id, lastname, firstname, photo FROM authors")
    assert.NoError(t, err, "Execuția interogării nu ar trebui să returneze o eroare")

    authors, err := ScanAuthors(result)

    assert.Error(t, err, "Era de așteptat o eroare după iterație")
    assert.Nil(t, authors, "Lista de autori ar trebui să fie nil la eroare")
}


func TestScanAuthors_ErrorDuringScan(t *testing.T) {
    db, mock, err := sqlmock.New()
    assert.NoError(t, err, "Error should be nil when creating sqlmock")
    defer db.Close()

    rows := sqlmock.NewRows([]string{"id", "lastname", "firstname", "photo"}).
        AddRow("invalid_id", "Doe", "John", "photo.jpg") 

    mock.ExpectQuery(`SELECT id, lastname, firstname, photo FROM authors`).WillReturnRows(rows)

    result, err := db.Query("SELECT id, lastname, firstname, photo FROM authors")
    assert.NoError(t, err, "Query execution should not return an error")

    authors, err := ScanAuthors(result)

    assert.Error(t, err, "Expected an error during scan")
    assert.Nil(t, authors, "Authors should be nil on error")
}

// TestValidateAuthorData tests the ValidateAuthorData function
func TestValidateAuthorData(t *testing.T) {
    author := Author{Firstname: "John", Lastname: "Doe"}
    err := ValidateAuthorData(author)
    assert.NoError(t, err, "Expected no error for valid author data")

    author = Author{Firstname: "", Lastname: "Doe"}
    err = ValidateAuthorData(author)
    assert.Error(t, err, "Expected an error for missing Firstname")
    assert.Contains(t, err.Error(), "firstname and lastname are required fields", "Error message should mention missing fields")

    author = Author{Firstname: "John", Lastname: ""}
    err = ValidateAuthorData(author)
    assert.Error(t, err, "Expected an error for missing Lastname")
    assert.Contains(t, err.Error(), "firstname and lastname are required fields", "Error message should mention missing fields")
}

// TestSearchAuthors_ErrorExecutingQuery tests the case where there is an error executing the SQL query
func TestSearchAuthors_ErrorExecutingQuery(t *testing.T) {
    app, mock := createTestApp(t)
    defer app.DB.Close()

    req, err := http.NewRequest("GET", "/authors?query=John", nil)
    assert.NoError(t, err, "Error should be nil when creating a new request")

    rr := httptest.NewRecorder()

    mock.ExpectQuery(`SELECT id, Firstname, Lastname, photo FROM authors WHERE Firstname LIKE \? OR Lastname LIKE \?`).
        WithArgs("%John%", "%John%").
        WillReturnError(fmt.Errorf("query execution error"))

    handler := http.HandlerFunc(app.SearchAuthors)
    handler.ServeHTTP(rr, req)

    assert.Equal(t, http.StatusInternalServerError, rr.Code, "Expected status code 500")
    assert.Contains(t, rr.Body.String(), "Error executing query", "Expected error message for query execution error")

    err = mock.ExpectationsWereMet()
    assert.NoError(t, err, "There should be no unmet expectations")
}

// TestSearchAuthors_ErrorScanningAuthors tests the case where there is an error scanning the rows
func TestSearchAuthors_ErrorScanningAuthors(t *testing.T) {
    app, mock := createTestApp(t)
    defer app.DB.Close()

    req, err := http.NewRequest("GET", "/authors?query=John", nil)
    assert.NoError(t, err, "Error should be nil when creating a new request")

    rr := httptest.NewRecorder()

    mock.ExpectQuery(`SELECT id, Firstname, Lastname, photo FROM authors WHERE Firstname LIKE \? OR Lastname LIKE \?`).
        WithArgs("%John%", "%John%").
        WillReturnRows(sqlmock.NewRows([]string{"id", "Firstname", "Lastname", "photo"}).
            AddRow("invalid_id", "John", "Doe", "photo.jpg")) 

    handler := http.HandlerFunc(app.SearchAuthors)
    handler.ServeHTTP(rr, req)

    assert.Equal(t, http.StatusInternalServerError, rr.Code, "Expected status code 500")
    assert.Contains(t, rr.Body.String(), "Error scanning authors", "Expected error message for scan error")

    err = mock.ExpectationsWereMet()
    assert.NoError(t, err, "There should be no unmet expectations")
}

func TestSearchAuthors_MissingQueryParameter(t *testing.T) {
    app, _ := createTestApp(t)
    defer app.DB.Close()

    req, err := http.NewRequest("GET", "/search_authors", nil)
    assert.NoError(t, err, "Error should be nil when creating a new request")

    rr := httptest.NewRecorder()

    handler := http.HandlerFunc(app.SearchAuthors)
    handler.ServeHTTP(rr, req)

    assert.Equal(t, http.StatusBadRequest, rr.Code, "Expected status code 400 for missing query parameter")
    assert.Contains(t, rr.Body.String(), "Query parameter is required", "Expected error message for missing query parameter")
}

func TestSearchAuthors_Success(t *testing.T) {
    app, mock := createTestApp(t)
    defer app.DB.Close()

    req, err := http.NewRequest("GET", "/authors?query=John", nil)
    assert.NoError(t, err, "Error should be nil when creating a new request")

    rr := httptest.NewRecorder()

    rows := sqlmock.NewRows([]string{"id", "lastname", "firstname", "photo"}).
        AddRow(1, "Doe", "John", "photo.jpg").
        AddRow(2, "Smith", "Jane", "photo2.jpg")

    mock.ExpectQuery(`SELECT id, Firstname, Lastname, photo FROM authors WHERE Firstname LIKE \? OR Lastname LIKE \?`).
        WithArgs("%John%", "%John%").
        WillReturnRows(rows)

    handler := http.HandlerFunc(app.SearchAuthors)
    handler.ServeHTTP(rr, req)

    assert.Equal(t, http.StatusOK, rr.Code, "Expected status code 200")

    expected := []map[string]interface{}{
        {"id": float64(1), "firstname": "John", "lastname": "Doe", "photo": "photo.jpg"},
        {"id": float64(2), "firstname": "Jane", "lastname": "Smith", "photo": "photo2.jpg"},
    }
    var actual []map[string]interface{}
    err = json.Unmarshal(rr.Body.Bytes(), &actual)
    assert.NoError(t, err, "Expected no error while unmarshaling JSON response")

    assert.Equal(t, expected, actual, "Expected JSON response")
}

func TestScanBooks(t *testing.T) {
    db, mock, err := sqlmock.New()
    assert.NoError(t, err, "Error should be nil when creating sqlmock")
    defer db.Close()

    rows := sqlmock.NewRows([]string{"book_id", "book_title", "author_id", "book_photo", "is_borrowed", "book_details", "author_lastname", "author_firstname"}).
        AddRow(1, "Sample Book", 1, "book.jpg", false, "A sample book", "Doe", "John").
        AddRow(2, "Another Book", 2, "another.jpg", true, "Another sample book", "Smith", "Jane")

    mock.ExpectQuery(`SELECT (.+) FROM books`).WillReturnRows(rows)

    result, err := db.Query("SELECT book_id, book_title, author_id, book_photo, is_borrowed, book_details, author_lastname, author_firstname FROM books")
    assert.NoError(t, err, "Query execution should not return an error")

    books, err := ScanBooks(result)
    assert.NoError(t, err, "Expected no error while scanning books")
    assert.Equal(t, 2, len(books), "Expected 2 books")
    assert.Equal(t, "Sample Book", books[0].BookTitle, "Expected BookTitle to be 'Sample Book'")
    assert.Equal(t, "Doe", books[0].AuthorLastname, "Expected AuthorLastname to be 'Doe'")
}

func TestSearchBooks_MissingQuery(t *testing.T) {
    
    app, _ := createTestApp(t)
    defer app.DB.Close()

    req, err := http.NewRequest("GET", "/books", nil)
    assert.NoError(t, err, "Error should be nil when creating a new request")

    rr := httptest.NewRecorder()

    handler := http.HandlerFunc(app.SearchBooks)
    handler.ServeHTTP(rr, req)

    assert.Equal(t, http.StatusBadRequest, rr.Code, "Expected status code 400")
    assert.Contains(t, rr.Body.String(), "Query parameter is required", "Expected error message for missing query parameter")
}

func TestSearchBooks_ErrorExecutingQuery(t *testing.T) {

    app, mock := createTestApp(t)
    defer app.DB.Close()

    req, err := http.NewRequest("GET", "/books?query=Sample", nil)
    assert.NoError(t, err, "Error should be nil when creating a new request")

    rr := httptest.NewRecorder()

    mock.ExpectQuery(`SELECT (.+) FROM books`).
        WithArgs("%Sample%", "%Sample%", "%Sample%").
        WillReturnError(fmt.Errorf("query execution error"))


    handler := http.HandlerFunc(app.SearchBooks)
    handler.ServeHTTP(rr, req)

    assert.Equal(t, http.StatusInternalServerError, rr.Code, "Expected status code 500")
    assert.Contains(t, rr.Body.String(), "Error executing query", "Expected error message for query execution error")
}

func TestSearchBooks_ErrorScanningRows(t *testing.T) {
    app, mock := createTestApp(t)
    defer app.DB.Close()
    req, err := http.NewRequest("GET", "/books?query=Sample", nil)
    assert.NoError(t, err, "Error should be nil when creating a new request")

    rr := httptest.NewRecorder()

    mock.ExpectQuery(`SELECT (.+) FROM books`).
        WithArgs("%Sample%", "%Sample%", "%Sample%").
        WillReturnRows(sqlmock.NewRows([]string{
            "book_id", "book_title", "author_id", "book_photo", "is_borrowed", "book_details", "author_lastname", "author_firstname",
        }).AddRow("invalid_id", "Sample Book", 1, "book.jpg", false, "A sample book", "Doe", "John")) // Valoare invalidă pentru a provoca o eroare

    handler := http.HandlerFunc(app.SearchBooks)
    handler.ServeHTTP(rr, req)

    assert.Equal(t, http.StatusInternalServerError, rr.Code, "Expected status code 500")
    assert.Contains(t, rr.Body.String(), "Error scanning books", "Expected error message for row scan error")
}

func TestSearchBooks_Success(t *testing.T) {

    app, mock := createTestApp(t)
    defer app.DB.Close()
    req, err := http.NewRequest("GET", "/books?query=Sample", nil)
    assert.NoError(t, err, "Error should be nil when creating a new request")

    rr := httptest.NewRecorder()
    rows := sqlmock.NewRows([]string{
        "book_id", "book_title", "author_id", "book_photo", "is_borrowed", "book_details", "author_lastname", "author_firstname",
    }).
        AddRow(1, "Sample Book", 1, "book.jpg", false, "A sample book", "Doe", "John").
        AddRow(2, "Another Book", 2, "another.jpg", true, "Another sample book", "Smith", "Jane")

    mock.ExpectQuery(`SELECT (.+) FROM books`).
        WithArgs("%Sample%", "%Sample%", "%Sample%").
        WillReturnRows(rows)

    handler := http.HandlerFunc(app.SearchBooks)
    handler.ServeHTTP(rr, req)

    assert.Equal(t, http.StatusOK, rr.Code, "Expected status code 200")

    var books []BookAuthorInfo
    err = json.NewDecoder(rr.Body).Decode(&books)
    assert.NoError(t, err, "Expected no error decoding JSON response")

    assert.Equal(t, 2, len(books), "Expected 2 books")
    assert.Equal(t, "Sample Book", books[0].BookTitle, "Expected BookTitle to be 'Sample Book'")
    assert.Equal(t, "Doe", books[0].AuthorLastname, "Expected AuthorLastname to be 'Doe'")
    assert.Equal(t, "John", books[0].AuthorFirstname, "Expected AuthorFirstname to be 'John'")
}

func TestScanBooks_ErrorAfterIteration(t *testing.T) {
    app, mock := createTestApp(t)
    defer app.DB.Close()


    rows := sqlmock.NewRows([]string{
        "book_id", "book_title", "author_id", "book_photo", "is_borrowed", "book_details", "author_lastname", "author_firstname",
    }).
        AddRow(1, "Sample Book", 1, "book.jpg", false, "A sample book", "Doe", "John").
        RowError(0, fmt.Errorf("iteration error")) 

    mock.ExpectQuery(`SELECT (.+) FROM books`).WillReturnRows(rows)

    result, err := app.DB.Query("SELECT (.+) FROM books")
    assert.NoError(t, err, "Expected no error when executing query")

    books, err := ScanBooks(result)

    assert.Error(t, err, "Expected an error after iteration")
    assert.Nil(t, books, "Books should be nil on error")

    if err := mock.ExpectationsWereMet(); err != nil {
        t.Errorf("Not all expectations were met: %v", err)
    }
}


/// TestGetAuthors tests the GetAuthors handler with Dependency Injection
func TestGetAuthors(t *testing.T) {
	app, mock := createTestApp(t)
	defer app.DB.Close()

	req, err := http.NewRequest("GET", "/authors", nil)
	assert.NoError(t, err, "Error should be nil when creating a new request")

	rr := httptest.NewRecorder()

	rows := sqlmock.NewRows([]string{"id", "lastname", "firstname", "photo"}).
		AddRow(1, "Doe", "John", "photo.jpg").
		AddRow(2, "Smith", "Jane", "photo2.jpg")

	mock.ExpectQuery(`SELECT id, Lastname, Firstname, photo FROM authors ORDER BY Lastname, Firstname`).
		WillReturnRows(rows)

	handler := http.HandlerFunc(app.GetAuthors)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "Expected status code 200")

	expected := []map[string]interface{}{
		{"id": float64(1), "lastname": "Doe", "firstname": "John", "photo": "photo.jpg"},
		{"id": float64(2), "lastname": "Smith", "firstname": "Jane", "photo": "photo2.jpg"},
	}
	var actual []map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &actual)
	assert.NoError(t, err, "Expected no error while unmarshaling JSON response")

	assert.Equal(t, expected, actual, "Expected JSON response")

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Not all expectations were met: %v", err)
	}
}

// TestGetAuthors_ErrorExecutingQuery tests the case where there is an error executing the SQL query in GetAuthors handler
func TestGetAuthors_ErrorExecutingQuery(t *testing.T) {
	app, mock := createTestApp(t)
	defer app.DB.Close()

	req, err := http.NewRequest("GET", "/authors", nil)
	assert.NoError(t, err, "Error should be nil when creating a new request")

	rr := httptest.NewRecorder()

	mock.ExpectQuery(`SELECT id, Lastname, Firstname, photo FROM authors ORDER BY Lastname, Firstname`).
		WillReturnError(fmt.Errorf("query execution error"))

	handler := http.HandlerFunc(app.GetAuthors)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code, "Expected status code 500")
	assert.Contains(t, rr.Body.String(), "Error executing query", "Expected error message for query execution error")

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Not all expectations were met: %v", err)
	}
}

// TestGetAuthors_ErrorScanningRows tests the case where there is an error scanning the rows in GetAuthors handler
func TestGetAuthors_ErrorScanningRows(t *testing.T) {
	app, mock := createTestApp(t)
	defer app.DB.Close()

	req, err := http.NewRequest("GET", "/authors", nil)
	assert.NoError(t, err, "Error should be nil when creating a new request")

	rr := httptest.NewRecorder()

	rows := sqlmock.NewRows([]string{"id", "lastname", "firstname", "photo"}).
		AddRow("invalid_id", "Doe", "John", "photo.jpg") 

	mock.ExpectQuery(`SELECT id, Lastname, Firstname, photo FROM authors ORDER BY Lastname, Firstname`).
		WillReturnRows(rows)

	handler := http.HandlerFunc(app.GetAuthors)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code, "Expected status code 500")
	assert.Contains(t, rr.Body.String(), "Error scanning authors", "Expected error message for row scan error")

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Not all expectations were met: %v", err)
	}
}


// TestGetAllBooks tests the GetAllBooks handler
func TestGetAllBooks_Success(t *testing.T) {
    app, mock := createTestApp(t)
    defer app.DB.Close()

    req, err := http.NewRequest("GET", "/books", nil)
    assert.NoError(t, err)

    rr := httptest.NewRecorder()

    rows := sqlmock.NewRows([]string{
        "book_id", "book_title", "author_id", "book_photo", "is_borrowed", "book_details", "author_lastname", "author_firstname",
    }).
        AddRow(1, "Sample Book", 1, "book.jpg", false, "A sample book", "Doe", "John").
        AddRow(2, "Another Book", 2, "another.jpg", true, "Another sample book", "Smith", "Jane")

    mock.ExpectQuery(`SELECT (.+) FROM books JOIN authors ON books.author_id = authors.id`).
        WillReturnRows(rows)

    handler := http.HandlerFunc(app.GetAllBooks)
    handler.ServeHTTP(rr, req)

    assert.Equal(t, http.StatusOK, rr.Code)

    var books []BookAuthorInfo
    err = json.NewDecoder(rr.Body).Decode(&books)
    assert.NoError(t, err)
    assert.Equal(t, 2, len(books))
    assert.Equal(t, "Sample Book", books[0].BookTitle)
    assert.Equal(t, "Doe", books[0].AuthorLastname)
    assert.Equal(t, "John", books[0].AuthorFirstname)
}

func TestGetAllBooks_ErrorQuery(t *testing.T) {
    app, mock := createTestApp(t)
    defer app.DB.Close()

    req, err := http.NewRequest("GET", "/books", nil)
    assert.NoError(t, err)

    rr := httptest.NewRecorder()

    mock.ExpectQuery(`SELECT (.+) FROM books JOIN authors ON books.author_id = authors.id`).
        WillReturnError(fmt.Errorf("database query error"))

    handler := http.HandlerFunc(app.GetAllBooks)
    handler.ServeHTTP(rr, req)

    assert.Equal(t, http.StatusInternalServerError, rr.Code)
    assert.Contains(t, rr.Body.String(), "Error executing query", "Expected 'Error executing query' in response")
}


func TestGetAllBooks_ErrorScan(t *testing.T) {
    app, mock := createTestApp(t)
    defer app.DB.Close()

    req, err := http.NewRequest("GET", "/books", nil)
    assert.NoError(t, err)

    rr := httptest.NewRecorder()

    rows := sqlmock.NewRows([]string{
        "book_id", "book_title", "author_id", "book_photo", "is_borrowed", "book_details", "author_lastname", "author_firstname",
    }).
        AddRow("invalid_id", "Sample Book", 1, "book.jpg", false, "A sample book", "Doe", "John")

    mock.ExpectQuery(`SELECT (.+) FROM books JOIN authors ON books.author_id = authors.id`).
        WillReturnRows(rows)

    handler := http.HandlerFunc(app.GetAllBooks)
    handler.ServeHTTP(rr, req)

    assert.Equal(t, http.StatusInternalServerError, rr.Code)
    assert.Contains(t, rr.Body.String(), "Error scanning books")
}


// TestGetAuthorsAndBooks tests the GetAuthorsAndBooks handler
func TestGetAuthorsAndBooks(t *testing.T) {
	app, mock := createTestApp(t)
	defer app.DB.Close()

	// Setting up SQL mock expectations
	rows := sqlmock.NewRows([]string{"author_firstname", "author_lastname", "book_title", "book_photo"}).
		AddRow("John", "Doe", "Book Title 1", "book1.jpg").
		AddRow("Jane", "Smith", "Book Title 2", "book2.jpg")

	mock.ExpectQuery("SELECT a.Firstname AS author_firstname, a.Lastname AS author_lastname, b.title AS book_title, b.photo AS book_photo").
		WillReturnRows(rows)

	// Creating a new HTTP request
	req, err := http.NewRequest("GET", "/authorsbooks", nil)
	if err != nil {
		t.Fatalf("Could not create request: %v", err)
	}

	// Capturing the response
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(app.GetAuthorsAndBooks)
	handler.ServeHTTP(rr, req)

	// Ensuring the response status is 200 OK
	assert.Equal(t, http.StatusOK, rr.Code)

	// Checking the JSON response
	var authorsAndBooks []AuthorBook
	err = json.NewDecoder(rr.Body).Decode(&authorsAndBooks)
	if err != nil {
		t.Fatalf("Could not decode response: %v", err)
	}

	// Verifying the response data
	assert.Equal(t, 2, len(authorsAndBooks))
	assert.Equal(t, "John", authorsAndBooks[0].AuthorFirstname)
	assert.Equal(t, "Doe", authorsAndBooks[0].AuthorLastname)
	assert.Equal(t, "Book Title 1", authorsAndBooks[0].BookTitle)
	assert.Equal(t, "book1.jpg", authorsAndBooks[0].BookPhoto)

	// Ensuring all mock expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Not all expectations were met: %v", err)
	}
}

// TestGetAuthorBooksByID tests the GetAuthorBooksByID handler
func TestGetAuthorBooksByID(t *testing.T) {
	app, mock := createTestApp(t)
	defer app.DB.Close()

	authorID := "1"

	// Setting up SQL mock expectations
	rows := sqlmock.NewRows([]string{"author_firstname", "author_lastname", "author_photo", "book_title", "book_photo"}).
		AddRow("John", "Doe", "john.jpg", "Book Title 1", "book1.jpg").
		AddRow("John", "Doe", "john.jpg", "Book Title 2", "book2.jpg")

	mock.ExpectQuery("SELECT a.Firstname AS author_firstname, a.Lastname AS author_lastname, a.Photo AS author_photo, b.title AS book_title, b.photo AS book_photo").
		WithArgs(1).
		WillReturnRows(rows)

	// Creating a new HTTP request
	req, err := http.NewRequest("GET", "/authors/1", nil)
	if err != nil {
		t.Fatalf("Could not create request: %v", err)
	}

	// Capturing the response
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(app.GetAuthorBooksByID)
	// Using mux.Vars to mock the ID parameter
	req = mux.SetURLVars(req, map[string]string{"id": authorID})
	handler.ServeHTTP(rr, req)

	// Ensuring the response status is 200 OK
	assert.Equal(t, http.StatusOK, rr.Code)

	// Checking the JSON response
	var authorAndBooks struct {
		AuthorFirstname string       `json:"author_firstname"`
		AuthorLastname  string       `json:"author_lastname"`
		AuthorPhoto     string       `json:"author_photo"`
		Books           []AuthorBook `json:"books"`
	}
	err = json.NewDecoder(rr.Body).Decode(&authorAndBooks)
	if err != nil {
		t.Fatalf("Could not decode response: %v", err)
	}

	// Verifying the response data
	assert.Equal(t, "John", authorAndBooks.AuthorFirstname)
	assert.Equal(t, "Doe", authorAndBooks.AuthorLastname)
	assert.Equal(t, "john.jpg", authorAndBooks.AuthorPhoto)
	assert.Equal(t, 2, len(authorAndBooks.Books))
	assert.Equal(t, "Book Title 1", authorAndBooks.Books[0].BookTitle)

	// Ensuring all mock expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Not all expectations were met: %v", err)
	}
}

// TestGetBookByID tests the GetBookByID handler
func TestGetBookByID(t *testing.T) {
	app, mock := createTestApp(t)
	defer app.DB.Close()

	bookID := "1"

	// Setting up SQL mock expectations
	rows := sqlmock.NewRows([]string{
		"book_title", "author_id", "book_photo", "is_borrowed", "book_id", "book_details", "author_lastname", "author_firstname",
	}).AddRow("Book Title", 1, "book.jpg", false, 1, "Book details", "Doe", "John")

	mock.ExpectQuery("SELECT books.title AS book_title, books.author_id AS author_id").
		WithArgs(1).
		WillReturnRows(rows)

	// Creating a new HTTP request
	req, err := http.NewRequest("GET", "/books/1", nil)
	if err != nil {
		t.Fatalf("Could not create request: %v", err)
	}

	// Capturing the response
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(app.GetBookByID)
	// Using mux.Vars to mock the ID parameter
	req = mux.SetURLVars(req, map[string]string{"id": bookID})
	handler.ServeHTTP(rr, req)

	// Ensuring the response status is 200 OK
	assert.Equal(t, http.StatusOK, rr.Code)

	// Checking the JSON response
	var book BookAuthorInfo
	err = json.NewDecoder(rr.Body).Decode(&book)
	if err != nil {
		t.Fatalf("Could not decode response: %v", err)
	}

	// Verifying the response data
	assert.Equal(t, "Book Title", book.BookTitle)
	assert.Equal(t, "Doe", book.AuthorLastname)
	assert.Equal(t, "John", book.AuthorFirstname)

	// Ensuring all mock expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Not all expectations were met: %v", err)
	}
}

// TestGetSubscribersByBookID tests the GetSubscribersByBookID handler
func TestGetSubscribersByBookID(t *testing.T) {
	app, mock := createTestApp(t)
	defer app.DB.Close()

	bookID := "1"

	// Setting up SQL mock expectations
	rows := sqlmock.NewRows([]string{"Lastname", "Firstname", "Email"}).
		AddRow("Doe", "John", "john.doe@example.com").
		AddRow("Smith", "Jane", "jane.smith@example.com")

	mock.ExpectQuery("SELECT s.Lastname, s.Firstname, s.Email").
		WithArgs(bookID).
		WillReturnRows(rows)

	// Creating a new HTTP request
	req, err := http.NewRequest("GET", "/subscribers/1", nil)
	if err != nil {
		t.Fatalf("Could not create request: %v", err)
	}

	// Capturing the response
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(app.GetSubscribersByBookID)
	// Using mux.Vars to mock the ID parameter
	req = mux.SetURLVars(req, map[string]string{"id": bookID})
	handler.ServeHTTP(rr, req)

	// Ensuring the response status is 200 OK
	assert.Equal(t, http.StatusOK, rr.Code)

	// Checking the JSON response
	var subscribers []Subscriber
	err = json.NewDecoder(rr.Body).Decode(&subscribers)
	if err != nil {
		t.Fatalf("Could not decode response: %v", err)
	}

	// Verifying the response data
	assert.Equal(t, 2, len(subscribers))
	assert.Equal(t, "Doe", subscribers[0].Lastname)
	assert.Equal(t, "john.doe@example.com", subscribers[0].Email)

	// Ensuring all mock expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Not all expectations were met: %v", err)
	}
}

// TestGetAllSubscribers tests the GetAllSubscribers handler
func TestGetAllSubscribers(t *testing.T) {
	app, mock := createTestApp(t)
	defer app.DB.Close()

	// Setting up SQL mock expectations
	rows := sqlmock.NewRows([]string{"lastname", "firstname", "email"}).
		AddRow("Doe", "John", "john.doe@example.com").
		AddRow("Smith", "Jane", "jane.smith@example.com")

	mock.ExpectQuery("SELECT lastname, firstname, email FROM subscribers").
		WillReturnRows(rows)

	// Creating a new HTTP request
	req, err := http.NewRequest("GET", "/subscribers", nil)
	if err != nil {
		t.Fatalf("Could not create request: %v", err)
	}

	// Capturing the response
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(app.GetAllSubscribers)
	handler.ServeHTTP(rr, req)

	// Ensuring the response status is 200 OK
	assert.Equal(t, http.StatusOK, rr.Code)

	// Checking the JSON response
	var subscribers []Subscriber
	err = json.NewDecoder(rr.Body).Decode(&subscribers)
	if err != nil {
		t.Fatalf("Could not decode response: %v", err)
	}

	// Verifying the response data
	assert.Equal(t, 2, len(subscribers))
	assert.Equal(t, "Doe", subscribers[0].Lastname)
	assert.Equal(t, "Smith", subscribers[1].Lastname)

	// Ensuring all mock expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Not all expectations were met: %v", err)
	}
}

// TestAddAuthorPhoto tests the AddAuthorPhoto handler
func TestAddAuthorPhoto(t *testing.T) {
	app, mock := createTestApp(t)
	defer app.DB.Close()

	authorID := "1"

	// Set up the SQL mock expectations
	mock.ExpectExec("^UPDATE authors SET photo = \\? WHERE id = \\?$").
		WithArgs("./upload/1/fullsize.jpg", 1).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Create a new HTTP request with a mocked file
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "test.jpg")
	if err != nil {
		t.Fatalf("Could not create form file: %v", err)
	}
	part.Write([]byte("test image content"))
	writer.Close()

	req, err := http.NewRequest("POST", "/author/photo/1", body)
	if err != nil {
		t.Fatalf("Could not create request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Capture the response
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(app.AddAuthorPhoto)
	// Use mux.Vars to mock the ID parameter
	req = mux.SetURLVars(req, map[string]string{"id": authorID})
	handler.ServeHTTP(rr, req)

	// Ensure the response status is 200 OK
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check the response message
	expected := "File uploaded successfully: ./upload/1/fullsize.jpg\n"
	assert.Equal(t, expected, rr.Body.String())

	// Ensure all mock expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Not all expectations were met: %v", err)
	}
}

// TestAddAuthor tests the AddAuthor handler
func TestAddAuthor(t *testing.T) {
	app, mock := createTestApp(t)
	defer app.DB.Close()

	// Setting up SQL mock expectations
	mock.ExpectExec("INSERT INTO authors").
		WithArgs("Doe", "John", "").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Creating a new HTTP request with JSON body
	author := Author{Firstname: "John", Lastname: "Doe"}
	body, err := json.Marshal(author)
	if err != nil {
		t.Fatalf("Could not marshal author: %v", err)
	}

	req, err := http.NewRequest("POST", "/authors/new", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("Could not create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Capturing the response
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(app.AddAuthor)
	handler.ServeHTTP(rr, req)

	// Ensuring the response status is 201 Created
	assert.Equal(t, http.StatusCreated, rr.Code)

	// Checking the JSON response
	var response map[string]int
	err = json.NewDecoder(rr.Body).Decode(&response)
	if err != nil {
		t.Fatalf("Could not decode response: %v", err)
	}

	// Verifying the response data
	assert.Equal(t, 1, response["id"])

	// Ensuring all mock expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Not all expectations were met: %v", err)
	}
}

// TestAddBookPhoto tests the AddBookPhoto handler
func TestAddBookPhoto(t *testing.T) {
	app, mock := createTestApp(t)
	defer app.DB.Close()

	bookID := "1"

	// Set up the SQL mock expectations
	mock.ExpectExec("^UPDATE books SET photo = \\? WHERE id = \\?$").
		WithArgs("./upload/books/1/fullsize.jpg", 1).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Create a new HTTP request with a mocked file
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "test.jpg")
	if err != nil {
		t.Fatalf("Could not create form file: %v", err)
	}
	part.Write([]byte("test image content"))
	writer.Close()

	req, err := http.NewRequest("POST", "/books/photo/1", body)
	if err != nil {
		t.Fatalf("Could not create request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Capture the response
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(app.AddBookPhoto)
	// Use mux.Vars to mock the ID parameter
	req = mux.SetURLVars(req, map[string]string{"id": bookID})
	handler.ServeHTTP(rr, req)

	// Ensure the response status is 200 OK
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check the response message
	expected := "File uploaded successfully: ./upload/books/1/fullsize.jpg\n"
	assert.Equal(t, expected, rr.Body.String())

	// Ensure all mock expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Not all expectations were met: %v", err)
	}
}

// TestAddBook tests the AddBook handler
func TestAddBook(t *testing.T) {
	app, mock := createTestApp(t)
	defer app.DB.Close()

	// Set up the SQL mock expectations
	mock.ExpectExec("INSERT INTO books").
		WithArgs("Test Book", "", "Some details", 1, false).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Create a new HTTP request with JSON body
	book := Book{Title: "Test Book", AuthorID: 1, Details: "Some details", IsBorrowed: false}
	body, err := json.Marshal(book)
	if err != nil {
		t.Fatalf("Could not marshal book: %v", err)
	}

	req, err := http.NewRequest("POST", "/books/new", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("Could not create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Capture the response
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(app.AddBook)
	handler.ServeHTTP(rr, req)

	// Ensure the response status is 201 Created
	assert.Equal(t, http.StatusCreated, rr.Code)

	// Check the JSON response
	var response map[string]int
	err = json.NewDecoder(rr.Body).Decode(&response)
	if err != nil {
		t.Fatalf("Could not decode response: %v", err)
	}

	// Verify the response data
	assert.Equal(t, 1, response["id"])

	// Ensure all mock expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Not all expectations were met: %v", err)
	}
}

// TestAddSubscriber tests the AddSubscriber handler
func TestAddSubscriber(t *testing.T) {
	app, mock := createTestApp(t)
	defer app.DB.Close()

	// Set up the SQL mock expectations
	mock.ExpectExec("INSERT INTO subscribers").
		WithArgs("Doe", "John", "john.doe@example.com").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Create a new HTTP request with JSON body
	subscriber := Subscriber{Firstname: "John", Lastname: "Doe", Email: "john.doe@example.com"}
	body, err := json.Marshal(subscriber)
	if err != nil {
		t.Fatalf("Could not marshal subscriber: %v", err)
	}

	req, err := http.NewRequest("POST", "/subscribers/new", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("Could not create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Capture the response
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(app.AddSubscriber)
	handler.ServeHTTP(rr, req)

	// Ensure the response status is 201 Created
	assert.Equal(t, http.StatusCreated, rr.Code)

	// Check the JSON response
	var response map[string]int
	err = json.NewDecoder(rr.Body).Decode(&response)
	if err != nil {
		t.Fatalf("Could not decode response: %v", err)
	}

	// Verify the response data
	assert.Equal(t, 1, response["id"])

	// Ensure all mock expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Not all expectations were met: %v", err)
	}
}

// TestBorrowBook tests the BorrowBook handler
func TestBorrowBook(t *testing.T) {
	app, mock := createTestApp(t)
	defer app.DB.Close()

	// Set up SQL mock expectations for checking if the book is borrowed
	mock.ExpectQuery("SELECT is_borrowed FROM books WHERE id = ?").
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"is_borrowed"}).AddRow(false))

	// Set up SQL mock expectations for inserting into borrowed_books
	mock.ExpectExec("INSERT INTO borrowed_books").
		WithArgs(1, 1).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Set up SQL mock expectations for updating the books table
	mock.ExpectExec("UPDATE books SET is_borrowed = TRUE WHERE id = ?").
		WithArgs(1).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Create a new HTTP request with JSON body
	requestBody := struct {
		SubscriberID int `json:"subscriber_id"`
		BookID       int `json:"book_id"`
	}{
		SubscriberID: 1,
		BookID:       1,
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("Could not marshal request body: %v", err)
	}

	req, err := http.NewRequest("POST", "/book/borrow", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("Could not create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Capture the response
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(app.BorrowBook)
	handler.ServeHTTP(rr, req)

	// Ensure the response status is 201 Created
	assert.Equal(t, http.StatusCreated, rr.Code)

	// Check the response message
	expected := `{"message": "Book borrowed successfully"}`
	assert.JSONEq(t, expected, rr.Body.String())

	// Ensure all mock expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Not all expectations were met: %v", err)
	}
}

// TestReturnBorrowedBook tests the ReturnBorrowedBook handler
func TestReturnBorrowedBook(t *testing.T) {
	app, mock := createTestApp(t)
	defer app.DB.Close()

	// Set up SQL mock expectations for checking if the book is borrowed
	mock.ExpectQuery("^SELECT is_borrowed FROM books WHERE id = \\?$").
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"is_borrowed"}).AddRow(true))

	// Set up SQL mock expectations for updating the borrowed_books table
	mock.ExpectExec("^UPDATE borrowed_books SET return_date = NOW\\(\\) WHERE subscriber_id = \\? AND book_id = \\?$").
		WithArgs(1, 1).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Set up SQL mock expectations for updating the books table
	mock.ExpectExec("^UPDATE books SET is_borrowed = FALSE WHERE id = \\?$").
		WithArgs(1).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Create a new HTTP request with JSON body
	requestBody := struct {
		SubscriberID int `json:"subscriber_id"`
		BookID       int `json:"book_id"`
	}{
		SubscriberID: 1,
		BookID:       1,
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("Could not marshal request body: %v", err)
	}

	req, err := http.NewRequest("POST", "/book/return", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("Could not create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Capture the response
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(app.ReturnBorrowedBook)
	handler.ServeHTTP(rr, req)

	// Ensure the response status is 200 OK
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check the response message
	expected := "Book returned successfully"
	assert.Equal(t, expected, rr.Body.String())

	// Ensure all mock expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Not all expectations were met: %v", err)
	}
}

// TestUpdateAuthor tests the UpdateAuthor handler
func TestUpdateAuthor(t *testing.T) {
	app, mock := createTestApp(t)
	defer app.DB.Close()

	authorID := "1"

	// Set up SQL mock expectations for updating the author
	mock.ExpectExec("^UPDATE authors SET lastname = \\?, firstname = \\?, photo = \\? WHERE id = \\?$").
		WithArgs("Doe", "John", "john.jpg", 1).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Create a new HTTP request with JSON body
	author := Author{Firstname: "John", Lastname: "Doe", Photo: "john.jpg"}
	body, err := json.Marshal(author)
	if err != nil {
		t.Fatalf("Could not marshal author: %v", err)
	}

	req, err := http.NewRequest("PUT", "/authors/1", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("Could not create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req = mux.SetURLVars(req, map[string]string{"id": authorID})

	// Capture the response
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(app.UpdateAuthor)
	handler.ServeHTTP(rr, req)

	// Ensure the response status is 200 OK
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check the response message
	expected := "Author updated successfully"
	assert.Equal(t, expected, rr.Body.String())

	// Ensure all mock expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Not all expectations were met: %v", err)
	}
}

// TestUpdateBook tests the UpdateBook handler
func TestUpdateBook(t *testing.T) {
	app, mock := createTestApp(t)
	defer app.DB.Close()

	bookID := "1"

	// Set up SQL mock expectations for updating the book
	mock.ExpectExec("^UPDATE books SET title = \\?, author_id = \\?, photo = \\?, details = \\?, is_borrowed = \\? WHERE id = \\?$").
		WithArgs("New Title", 1, "newphoto.jpg", "Some details", false, 1).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Create a new HTTP request with JSON body
	book := struct {
		Title      string `json:"title"`
		AuthorID   int    `json:"author_id"`
		Photo      string `json:"photo"`
		Details    string `json:"details"`
		IsBorrowed bool   `json:"is_borrowed"`
	}{
		Title:      "New Title",
		AuthorID:   1,
		Photo:      "newphoto.jpg",
		Details:    "Some details",
		IsBorrowed: false,
	}
	body, err := json.Marshal(book)
	if err != nil {
		t.Fatalf("Could not marshal book: %v", err)
	}

	req, err := http.NewRequest("PUT", "/books/1", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("Could not create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req = mux.SetURLVars(req, map[string]string{"id": bookID})

	// Capture the response
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(app.UpdateBook)
	handler.ServeHTTP(rr, req)

	// Ensure the response status is 200 OK
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check the response message
	expected := "Book updated successfully"
	assert.Equal(t, expected, rr.Body.String())

	// Ensure all mock expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Not all expectations were met: %v", err)
	}
}




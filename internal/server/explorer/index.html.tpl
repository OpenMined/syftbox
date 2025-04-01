<!DOCTYPE html>
<html>

<head>
    <title>Index of /{{.Path}}</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            padding: 20px;
        }

        a {
            text-decoration: none;
            color: blue;
        }

        table {
            width: 100%;
            border-collapse: collapse;
        }

        th,
        td {
            padding: 8px;
            text-align: left;
            border-bottom: 1px solid #ddd;
        }

        th {
            background-color: #f4f4f4;
        }
    </style>
</head>

<body>
    <h1>Index of /{{.Path}}</h1>
    <table>
        <tr>
            <th>Name</th>
            <th>Size</th>
            <th>Last Modified</th>
        </tr>
        <tr>
            <td><a href="../">.. (parent directory)</a></td>
            <td></td>
            <td></td>
        </tr>
        {{range .Folders}}<tr>
            <td><a href="./{{.}}/">{{.}}</a></td>
            <td>DIR</td>
            <td></td>
        </tr>{{end}}
        {{range .Files}}<tr>
            <td><a href="./{{basename .Key}}">{{basename .Key}}</a></td>
            <td>{{.Size}}</td>
            <td>{{.LastModified}}</td>
        </tr>{{end}}
    </table>
</body>

</html>